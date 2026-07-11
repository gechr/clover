package main

import (
	"bytes"
	"context"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDescribeFallsBackToRef confirms describe returns the bare ref when src is
// not a git checkout (rev-parse fails), the only branch that is hermetic.
func TestDescribeFallsBackToRef(t *testing.T) {
	t.Parallel()
	require.Equal(t, "main", describe(context.Background(), t.TempDir(), "main"))
}

func TestBackendSpec(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		backend any
		want    string
	}{
		"plain string": {backend: "github:owner/repo", want: "github:owner/repo"},
		"table with full": {
			backend: map[string]any{"full": "ubi:owner/tool"},
			want:    "ubi:owner/tool",
		},
		"table without":    {backend: map[string]any{"options": "x"}, want: ""},
		"non-string field": {backend: map[string]any{"full": 7}, want: ""},
		"unsupported kind": {backend: 42, want: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, backendSpec(tt.backend))
		})
	}
}

func TestRepository(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		backends []any
		want     string
		ok       bool
	}{
		"github backend": {
			backends: []any{"github:owner/repo"},
			want:     "owner/repo",
			ok:       true,
		},
		"aqua backend": {
			backends: []any{"aqua:owner/repo"},
			want:     "owner/repo",
			ok:       true,
		},
		"ubi backend": {
			backends: []any{"ubi:owner/repo"},
			want:     "owner/repo",
			ok:       true,
		},
		"option qualifier dropped": {
			backends: []any{"ubi:owner/repo[exe=rg]"},
			want:     "owner/repo",
			ok:       true,
		},
		"monorepo sub-path":   {backends: []any{"github:owner/repo/sub"}, ok: false},
		"domain-shaped owner": {backends: []any{"aqua:atlassian.com/acli"}, ok: false},
		"empty list":          {backends: nil, ok: false},
		"no github backend":   {backends: []any{"npm:pkg", "cargo:crate"}, ok: false},
		"first github wins": {
			backends: []any{"npm:pkg", "github:first/one", "github:second/two"},
			want:     "first/one",
			ok:       true,
		},
		"table backend": {
			backends: []any{map[string]any{"full": "github:owner/repo"}},
			want:     "owner/repo",
			ok:       true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo, ok := repository(tt.backends)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, repo)
		})
	}
}

func TestBody(t *testing.T) {
	t.Parallel()

	require.Equal(t, []byte("second\nthird\n"), body([]byte("first\nsecond\nthird\n")))
	require.Empty(t, body([]byte("only-one-line")), "a single line with no newline drops to empty")
}

func TestRender(t *testing.T) {
	t.Parallel()

	out, err := render(map[string]string{"zebra": "z/zebra", "apple": "a/apple"}, "main@abc1234")
	require.NoError(t, err)

	source := string(out)
	require.Contains(t, source, "mise registry (main@abc1234)", "the header records the ref")
	require.Contains(t, source, "package match")

	// Keys are emitted in sorted order.
	apple := strings.Index(source, `"apple"`)
	zebra := strings.Index(source, `"zebra"`)
	require.Positive(t, apple)
	require.Less(t, apple, zebra, "keys are sorted")
	require.Contains(t, source, `"apple": "a/apple",`)
	require.Contains(t, source, `"zebra": "z/zebra",`)

	// The output is gofmt-canonical, so re-formatting is a no-op.
	reformatted, err := format.Source(out)
	require.NoError(t, err)
	require.Equal(t, out, reformatted)
}

func TestRead(t *testing.T) {
	t.Parallel()

	t.Run("empty dir errors", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, err := read(dir)
		require.EqualError(t, err, `no registry files under "`+dir+`"`)
	})

	t.Run("malformed toml errors", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(
			t,
			os.WriteFile(filepath.Join(dir, "bad.toml"), []byte("backends = ["), 0o600),
		)
		_, err := read(dir)
		require.Error(t, err)
		require.Contains(t, err.Error(), "parse")
	})

	t.Run("maps tools, skips curated and non-github, adds aliases", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeToml := func(name, body string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
		}
		writeToml(
			"ripgrep.toml",
			"backends = [\"github:BurntSushi/ripgrep\"]\naliases = [\"rg\"]\n",
		)
		writeToml(
			"terraform.toml",
			"backends = [\"github:hashicorp/terraform\"]\n",
		) // curated, skipped
		writeToml(
			"leftpad.toml",
			"backends = [\"npm:leftpad\"]\n",
		) // no github backend

		tools, err := read(dir)
		require.NoError(t, err)
		require.Equal(t, map[string]string{
			"ripgrep": "BurntSushi/ripgrep",
			"rg":      "BurntSushi/ripgrep", // alias
		}, tools)
	})
}

// TestReadOutputRenders confirms read output feeds render without a format error.
func TestReadOutputRenders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "gh.toml"),
		[]byte("backends = [\"github:cli/cli\"]\n"), 0o600))

	tools, err := read(dir)
	require.NoError(t, err)
	out, err := render(tools, "main")
	require.NoError(t, err)
	require.True(t, bytes.Contains(out, []byte(`"gh": "cli/cli",`)))
}
