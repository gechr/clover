package sidecar_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/sidecar"
	"github.com/stretchr/testify/require"
)

func TestTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		wantTarget string
		wantOK     bool
	}{
		{"tsconfig.json.clover.yaml", "tsconfig.json", true},
		{"tsconfig.json.clover.yml", "tsconfig.json", true},
		{"package.json.clover.yaml", "package.json", true},
		{".clover.yaml", "", false},  // the bare config, not a sidecar
		{".clover.yml", "", false},   // the bare config, not a sidecar
		{"config.yaml", "", false},   // a plain YAML file
		{"tsconfig.json", "", false}, // the target itself
		{"notes.clover.txt", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target, ok := sidecar.Target(tc.name)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantTarget, target)
		})
	}
}

func TestNames(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		[]string{"tsconfig.json.clover.yaml", "tsconfig.json.clover.yml"},
		sidecar.Names("tsconfig.json"),
		".yaml precedes .yml",
	)
}

func TestEntries(t *testing.T) {
	t.Parallel()

	data := []byte("" +
		"- provider: github\n" +
		"  repository: biomejs/biome\n" +
		"  find: schemas/<version>/schema.json\n" +
		"- provider: github\n" +
		"  repository: a/b\n" +
		"  find: v<version>\n")

	entries, err := sidecar.Entries(data)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.NoError(t, entries[0].Err)
	repo, _ := entries[0].Directive.Get("repository")
	require.Equal(t, "biomejs/biome", repo)
	require.Equal(t, 1, entries[0].Line, "the first entry starts on source line 1")
}

func TestEntriesNonList(t *testing.T) {
	t.Parallel()

	_, err := sidecar.Entries([]byte("provider: github\nrepository: a/b\n"))
	require.EqualError(t, err, "sidecar must be a YAML list of entries")
}

func TestEntriesPerEntryError(t *testing.T) {
	t.Parallel()

	// A sequence value on a non-repeatable key is rejected for that one entry,
	// without failing the whole document.
	data := []byte("- repository:\n    - a\n    - b\n")
	entries, err := sidecar.Entries(data)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.EqualError(t, entries[0].Err, `"repository" does not accept a list value`)
}

func TestEntriesEmpty(t *testing.T) {
	t.Parallel()

	entries, err := sidecar.Entries(nil)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestLocateFind(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{`,
		`  "$schema": "https://biomejs.dev/schemas/1.5.3/schema.json",`,
		`  "version": "1.5.3"`,
		`}`,
	}
	d := directive.Directive{Pairs: []directive.KV{
		{Key: "find", Value: "schemas/<version>/schema.json"},
	}}

	line, err := sidecar.Locate(lines, d)
	require.NoError(t, err)
	require.Equal(t, 1, line, "the $schema line, not the version line")
}

func TestLocateFindZeroMatches(t *testing.T) {
	t.Parallel()

	d := directive.Directive{Pairs: []directive.KV{{Key: "find", Value: "nonesuch"}}}
	_, err := sidecar.Locate([]string{"a", "b"}, d)
	require.EqualError(t, err, "find matched no line")
}

func TestLocateFindAmbiguous(t *testing.T) {
	t.Parallel()

	lines := []string{`"v": "1.0.0"`, `"w": "2.0.0"`}
	d := directive.Directive{Pairs: []directive.KV{{Key: "find", Value: `<version>`}}}
	_, err := sidecar.Locate(lines, d)
	require.EqualError(t, err, "find matched 2 lines - make it more specific")
}

func TestLocateMissingLocator(t *testing.T) {
	t.Parallel()

	d := directive.Directive{Pairs: []directive.KV{{Key: "provider", Value: "github"}}}
	_, err := sidecar.Locate(nil, d)
	require.EqualError(t, err, `needs a "find" or "jq" locator`)
}

// TestLocateFindEmpty is the defense-in-depth guard: even if an empty find
// reaches the locator, it errors rather than compiling to a match-all pattern.
func TestLocateFindEmpty(t *testing.T) {
	t.Parallel()

	d := directive.Directive{Pairs: []directive.KV{{Key: "find", Value: ""}}}
	_, err := sidecar.Locate([]string{"a", "b"}, d)
	require.EqualError(t, err, `"find" pattern is empty`)
}

func TestLocateJQEmpty(t *testing.T) {
	t.Parallel()

	d := directive.Directive{Pairs: []directive.KV{{Key: "jq", Value: ""}}}
	_, err := sidecar.Locate([]string{"{}"}, d)
	require.EqualError(t, err, `"jq" expression is empty`)
}
