package version_test

import (
	"testing"

	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

func TestNewConstraintErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		current string // empty means nil current passed in
		wantErr bool
	}{
		{name: "keyword needs current", expr: "minor", current: "", wantErr: true},
		{name: "keyword with current", expr: "minor", current: "1.2.3"},
		{name: "range ignores missing current", expr: ">=1.2,<2.0", current: ""},
		{name: "unparseable range", expr: "not-a-range", current: "1.2.3", wantErr: true},
		{name: "pessimistic range", expr: "~>1.4", current: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var current *version.Version
			if tt.current != "" {
				current = mustParse(t, tt.current)
			}

			c, err := version.NewConstraint(tt.expr, current)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, c)
		})
	}
}

func TestConstraintAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		expr      string
		current   string
		candidate string
		want      bool
	}{
		// keyword: patch ceiling - major and minor must match.
		{
			name:      "patch allows newer patch",
			expr:      "patch",
			current:   "1.2.3",
			candidate: "1.2.9",
			want:      true,
		},
		{
			name:      "patch rejects minor bump",
			expr:      "patch",
			current:   "1.2.3",
			candidate: "1.3.0",
			want:      false,
		},
		{
			name:      "patch rejects major bump",
			expr:      "patch",
			current:   "1.2.3",
			candidate: "2.0.0",
			want:      false,
		},
		// keyword: minor ceiling - only major must match.
		{
			name:      "minor allows minor bump",
			expr:      "minor",
			current:   "1.2.3",
			candidate: "1.5.0",
			want:      true,
		},
		{
			name:      "minor allows patch bump",
			expr:      "minor",
			current:   "1.2.3",
			candidate: "1.2.9",
			want:      true,
		},
		{
			name:      "minor rejects major bump",
			expr:      "minor",
			current:   "1.2.3",
			candidate: "2.0.0",
			want:      false,
		},
		// keyword: major ceiling - anything goes.
		{
			name:      "major allows major bump",
			expr:      "major",
			current:   "1.2.3",
			candidate: "9.0.0",
			want:      true,
		},
		// keyword ceilings are not downgrade guards (that lives in the chain).
		{
			name:      "patch allows older patch",
			expr:      "patch",
			current:   "1.2.3",
			candidate: "1.2.0",
			want:      true,
		},
		// range: standard Terraform-style expressions.
		{name: "range in bounds", expr: ">=1.2,<2.0", current: "", candidate: "1.9.0", want: true},
		{
			name:      "range above ceiling",
			expr:      ">=1.2,<2.0",
			current:   "",
			candidate: "2.0.0",
			want:      false,
		},
		{name: "pessimistic in range", expr: "~>1.4", current: "", candidate: "1.9.0", want: true},
		{
			name:      "pessimistic out of range",
			expr:      "~>1.4",
			current:   "",
			candidate: "2.0.0",
			want:      false,
		},
		{name: "not-equal excludes", expr: "!=1.2.3", current: "", candidate: "1.2.3", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var current *version.Version
			if tt.current != "" {
				current = mustParse(t, tt.current)
			}

			c, err := version.NewConstraint(tt.expr, current)
			require.NoError(t, err)
			require.Equal(t, tt.want, c.Allowed(mustParse(t, tt.candidate)))
		})
	}
}

func TestNilConstraintAllowsEverything(t *testing.T) {
	t.Parallel()

	var c *version.Constraint
	require.True(t, c.Allowed(mustParse(t, "999.0.0")))
}
