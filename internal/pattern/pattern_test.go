package pattern_test

import (
	"testing"

	"github.com/gechr/clover/internal/pattern"
	"github.com/stretchr/testify/require"
)

func TestCompileKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want pattern.Kind
	}{
		{name: "bare is glob", raw: "1.2.3", want: pattern.KindGlob},
		{name: "wildcard is glob", raw: "v*", want: pattern.KindGlob},
		{name: "lone slash is glob", raw: "/", want: pattern.KindGlob},
		{name: "delimited is regex", raw: "/v.*/", want: pattern.KindRegex},
		{name: "empty regex", raw: "//", want: pattern.KindRegex},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p, err := pattern.Compile(tt.raw)
			require.NoError(t, err)
			require.Equal(t, tt.want, p.Kind())
			require.Equal(t, tt.raw, p.String())
		})
	}
}

func TestCompileErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "invalid regex", raw: "/(/"},
		{name: "unterminated char class", raw: "[a-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := pattern.Compile(tt.raw)
			require.Error(t, err)
		})
	}
}

func TestMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		subject string
		want    bool
	}{
		// glob: bare value is exact (no separate literal kind needed).
		{name: "exact matches", raw: "1.2.3", subject: "1.2.3", want: true},
		{name: "exact rejects superstring", raw: "1.2.3", subject: "1.2.3-rc1", want: false},
		// glob: prefix and wildcards.
		{name: "prefix star", raw: "v*", subject: "v1.27.0", want: true},
		{name: "prefix star rejects", raw: "v*", subject: "1.27.0", want: false},
		{name: "question single char", raw: "1.?", subject: "1.9", want: true},
		{name: "question rejects multi", raw: "1.?", subject: "1.99", want: false},
		{name: "char class", raw: "1.[0-9]", subject: "1.7", want: true},
		// glob: whole-string - * spans / (opaque token, not a path).
		{name: "star spans slash", raw: "*", subject: "subdir/v1.2.3", want: true},
		{name: "prefix spans slash", raw: "sub*", subject: "subdir/v1.2.3", want: true},
		// glob: backslash escapes a metacharacter to match it literally.
		{name: "escaped star literal", raw: `1.2\*`, subject: "1.2*", want: true},
		{name: "escaped star not wildcard", raw: `1.2\*`, subject: "1.2.3", want: false},
		// regex: unanchored substring search.
		{name: "regex substring", raw: "/alpine/", subject: "1.27-alpine", want: true},
		{name: "regex anchored end", raw: "/-alpine$/", subject: "1.27-alpine", want: true},
		{
			name:    "regex anchored end rejects",
			raw:     "/-alpine$/",
			subject: "1.27-alpine-slim",
			want:    false,
		},
		{name: "regex anchored start", raw: "/^v/", subject: "v1.2.3", want: true},
		// regex: escaped delimiter matches a literal slash.
		{name: "regex escaped slash", raw: `/a\/b/`, subject: "a/b", want: true},
		// regex: empty pattern matches anything.
		{name: "empty regex matches", raw: "//", subject: "anything", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p, err := pattern.Compile(tt.raw)
			require.NoError(t, err)
			require.Equal(t, tt.want, p.Matches(tt.subject))
		})
	}
}
