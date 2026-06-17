package directive_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/stretchr/testify/require"
)

func TestParseIgnore(t *testing.T) {
	tests := []struct {
		body string
		want directive.IgnoreScope
	}{
		{"clover:ignore", directive.IgnoreNextLine},
		{"  clover:ignore  ", directive.IgnoreNextLine}, // surrounding space tolerated
		{"clover:ignore-file", directive.IgnoreFile},
		{
			"clover:ignore-file because it is test data",
			directive.IgnoreFile,
		}, // trailing note allowed
		{"clover:ignore-start", directive.IgnoreBlockStart},
		{"clover:ignore-end", directive.IgnoreBlockEnd},
		{"clover: provider=github", directive.IgnoreNone}, // ordinary directive
		{"clover:ignored=true", directive.IgnoreNone},     // not a control, just a key
		{"not a directive", directive.IgnoreNone},
	}

	for _, tc := range tests {
		t.Run(tc.body, func(t *testing.T) {
			require.Equal(t, tc.want, directive.ParseIgnore(tc.body))
		})
	}
}
