package sidecar_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/sidecar"
	"github.com/stretchr/testify/require"
)

// renderKeys is a stub keysFor: it maps the github provider to a fixed key list
// and every other provider (including the empty name) to none, so these tests
// never depend on the provider registry.
func renderKeys(provider string) []string {
	if provider == "github" {
		return []string{"repository", "host"}
	}
	return nil
}

// TestRender confirms directives serialize to a canonical YAML sidecar document
// in canonical key order (provider first, then the provider's keys, then the
// locator), regardless of the order the pairs were written.
func TestRender(t *testing.T) {
	t.Parallel()

	entries := []directive.Directive{{Pairs: []directive.KV{
		{Key: "find", Value: "v<version>"},
		{Key: "repository", Value: "owner/name"},
		{Key: "provider", Value: "github"},
	}}}

	out, err := sidecar.Render(entries, renderKeys)
	require.NoError(t, err)
	require.Equal(t,
		"- provider: github\n  repository: owner/name\n  find: v<version>\n",
		string(out),
	)
}

// TestRenderEmpty confirms an empty entry list renders the empty sequence, which
// decodes back to no entries.
func TestRenderEmpty(t *testing.T) {
	t.Parallel()

	out, err := sidecar.Render(nil, renderKeys)
	require.NoError(t, err)
	require.Equal(t, "[]\n", string(out))

	entries, err := sidecar.Entries(out)
	require.NoError(t, err)
	require.Empty(t, entries)
}

// TestRenderNoProviderKey confirms an entry without a provider key still renders,
// with keysFor consulted for the empty provider name (which supplies no keys).
func TestRenderNoProviderKey(t *testing.T) {
	t.Parallel()

	entries := []directive.Directive{{Pairs: []directive.KV{
		{Key: "find", Value: "v<version>"},
	}}}

	out, err := sidecar.Render(entries, renderKeys)
	require.NoError(t, err)
	require.Equal(t, "- find: v<version>\n", string(out))
}

// TestCanonicalize walks the re-emit outcomes: an already-canonical document is
// unchanged, an out-of-order one is reordered, comments survive the reorder, an
// unknown key is rejected without prune and pruned with it, and a structurally
// broken document is left untouched (zero value, no error) since lint owns those
// diagnostics.
func TestCanonicalize(t *testing.T) {
	t.Parallel()

	canonical := "- provider: github\n  repository: owner/name\n  find: v<version>\n"

	tests := map[string]struct {
		data        string
		prune       bool
		wantContent string
		wantChanged bool
		wantPruned  []string
		wantErr     string
	}{
		"already canonical": {
			data:        canonical,
			wantContent: canonical,
			wantChanged: false,
		},
		"out of order keys": {
			data:        "- find: v<version>\n  provider: github\n  repository: owner/name\n",
			wantContent: canonical,
			wantChanged: true,
		},
		"per-key comments survive reordering": {
			data:        "- find: v<version> # loc\n  provider: github\n  repository: owner/name # repo note\n",
			wantContent: "- provider: github\n  repository: owner/name # repo note\n  find: v<version> # loc\n",
			wantChanged: true,
		},
		"unknown key without prune": {
			data:    "- provider: github\n  repository: owner/name\n  find: v<version>\n  bogus: x\n",
			wantErr: `unknown key "bogus"`,
		},
		"unknown key with prune drops it and its comment": {
			data:        "- provider: github\n  repository: owner/name\n  find: v<version>\n  bogus: x # gone\n",
			prune:       true,
			wantContent: canonical,
			wantChanged: true,
			wantPruned:  []string{"bogus"},
		},
		"unparseable yaml": {
			data:        "[unclosed",
			wantContent: "",
			wantChanged: false,
		},
		"empty document": {
			data:        "",
			wantContent: "",
			wantChanged: false,
		},
		"mapping root": {
			data:        "provider: github\nrepository: owner/name\n",
			wantContent: "",
			wantChanged: false,
		},
		"malformed entry with list valued key": {
			data:        "- repository:\n    - a\n    - b\n",
			wantContent: "",
			wantChanged: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := sidecar.Canonicalize([]byte(tc.data), renderKeys, tc.prune)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				require.Equal(t, sidecar.Canonical{}, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantContent, string(got.Content))
			require.Equal(t, tc.wantChanged, got.Changed)
			require.Equal(t, tc.wantPruned, got.Pruned)
		})
	}
}

// TestRefreshPatchesLocatedEntry confirms a located entry whose refresh returns a
// replacement is re-rendered canonically, and that refresh is handed the entry's
// 0-based located line.
func TestRefreshPatchesLocatedEntry(t *testing.T) {
	t.Parallel()

	src := []byte("- provider: github\n  repository: owner/name\n  find: v<version>\n")
	lines := []string{"tag v1.2.3 here", "other"}

	var gotLine int
	refresh := func(line int, _ directive.Directive) (directive.Directive, bool) {
		gotLine = line
		return directive.Directive{Pairs: []directive.KV{
			{Key: "provider", Value: "github"},
			{Key: "repository", Value: "owner/name"},
			{Key: "find", Value: "v<version>"},
			{Key: "constraint", Value: "minor"},
		}}, true
	}

	out, err := sidecar.Refresh(src, lines, renderKeys, refresh, nil)
	require.NoError(t, err)
	require.Equal(t, 0, gotLine, "refresh receives the located line of the entry")
	require.Equal(t,
		"- provider: github\n  repository: owner/name\n  find: v<version>\n  constraint: minor\n",
		string(out),
	)
}

// TestRefreshKeepsEntryVerbatim confirms an entry is emitted unchanged when
// refresh declines it, when it fails to parse, and when it locates no line - a
// force pass never drops an entry it cannot reproduce.
func TestRefreshKeepsEntryVerbatim(t *testing.T) {
	t.Parallel()

	keep := func(int, directive.Directive) (directive.Directive, bool) {
		return directive.Directive{}, false
	}
	patch := func(int, directive.Directive) (directive.Directive, bool) {
		return directive.Directive{Pairs: []directive.KV{{Key: "provider", Value: "github"}}}, true
	}

	tests := map[string]struct {
		src     string
		lines   []string
		refresh func(int, directive.Directive) (directive.Directive, bool)
	}{
		"refresh declines": {
			src:     "- provider: github\n  repository: owner/name\n  find: v<version>\n",
			lines:   []string{"tag v1.2.3 here"},
			refresh: keep,
		},
		"malformed entry": {
			src:     "- repository:\n    - a\n    - b\n",
			lines:   []string{"anything"},
			refresh: patch,
		},
		"entry locates no line": {
			src:     "- provider: github\n  repository: owner/name\n  find: nonesuch<version>\n",
			lines:   []string{"tag v1.2.3 here"},
			refresh: patch,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := sidecar.Refresh([]byte(tc.src), tc.lines, renderKeys, tc.refresh, nil)
			require.NoError(t, err)
			require.Equal(t, tc.src, string(out), "the entry is kept verbatim")
		})
	}
}

// TestRefreshAppendsFresh confirms fresh entries are appended, and that a
// non-sequence document is given a new sequence root to hold them.
func TestRefreshAppendsFresh(t *testing.T) {
	t.Parallel()

	fresh := []directive.Directive{{Pairs: []directive.KV{
		{Key: "provider", Value: "github"},
		{Key: "repository", Value: "a/b"},
		{Key: "find", Value: "x<version>"},
	}}}
	refresh := func(int, directive.Directive) (directive.Directive, bool) {
		return directive.Directive{}, false
	}

	out, err := sidecar.Refresh([]byte("{}"), nil, renderKeys, refresh, fresh)
	require.NoError(t, err)
	require.Equal(t,
		"- provider: github\n  repository: a/b\n  find: x<version>\n",
		string(out),
	)
}

// TestRefreshUnparseableYAML confirms a document that does not parse as YAML is a
// hard error, unlike Canonicalize which stays silent for lint to report.
func TestRefreshUnparseableYAML(t *testing.T) {
	t.Parallel()

	refresh := func(int, directive.Directive) (directive.Directive, bool) {
		return directive.Directive{}, false
	}
	_, err := sidecar.Refresh([]byte("[unclosed"), nil, renderKeys, refresh, nil)
	require.Error(t, err)
}
