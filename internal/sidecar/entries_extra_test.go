package sidecar_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/sidecar"
	"github.com/stretchr/testify/require"
)

// TestEntriesInvalidYAML confirms input that does not parse as YAML is a hard
// error rather than an empty entry list.
func TestEntriesInvalidYAML(t *testing.T) {
	t.Parallel()

	_, err := sidecar.Entries([]byte("[unclosed"))
	require.Error(t, err)
}

// TestEntriesWhitespaceOnly confirms whitespace-only input parses to no node and
// yields no entries without error.
func TestEntriesWhitespaceOnly(t *testing.T) {
	t.Parallel()

	entries, err := sidecar.Entries([]byte("   \n  \n"))
	require.NoError(t, err)
	require.Empty(t, entries)
}

// TestLocateFindInvalidRegex confirms a find whose /regex/ does not compile is
// surfaced as a find error rather than matching nothing.
func TestLocateFindInvalidRegex(t *testing.T) {
	t.Parallel()

	d := directive.Directive{Pairs: []directive.KV{{Key: "find", Value: "/[/"}}}
	_, err := sidecar.Locate([]string{"a", "b"}, d)
	require.EqualError(
		t,
		err,
		"\"find\": compile regex pattern \"/[/\": error parsing regexp: missing closing ]: `[`",
	)
}
