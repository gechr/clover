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
		// glob with a <token>: filters like a wildcard run (the monorepo case).
		{name: "token matches", raw: "sg2-test/<version>", subject: "sg2-test/v1.7.19", want: true},
		{
			name:    "token rejects sibling",
			raw:     "sg2-test/<version>",
			subject: "sg2-fuzz/v0.1.2",
			want:    false,
		},
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

func TestCapture(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		glob       string
		line       string
		wantTokens []string
		wantValue  string // text of capture group 1, "" when no match expected
	}{
		{"version token", "<version>", "FROM nginx:1.27", []string{"version"}, "1.27"},
		{
			"literal context",
			"catalyst-<version>-linux",
			"x catalyst-1.2.3-linux y",
			[]string{"version"},
			"1.2.3",
		},
		{
			"two tokens",
			"<major.minor.patch>*<hex>.tgz",
			"tool-1.2.3-deadbeef.tgz",
			[]string{"major.minor.patch", "hex"},
			"1.2.3",
		},
		{"dot is literal", "app.<major.minor>", "app.1.27", []string{"major.minor"}, "1.27"},
		{
			"build metadata",
			"img:<version>",
			"img:1.2.3+build.5",
			[]string{"version"},
			"1.2.3+build.5",
		},
		{"no match", "<version>", "no version here", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p, err := pattern.Compile(tt.glob)
			require.NoError(t, err)
			require.Equal(t, pattern.KindGlob, p.Kind())
			if tt.wantTokens != nil {
				require.Equal(t, tt.wantTokens, p.Tokens())
			}

			m := p.Regexp().FindStringSubmatch(tt.line)
			if tt.wantValue == "" {
				require.Nil(t, m)
				return
			}
			require.Equal(t, tt.wantValue, m[1])
		})
	}
}

func TestCaptureNonGreedy(t *testing.T) {
	t.Parallel()

	// A non-greedy * does not starve the trailing <hex>: it captures deadbeef in
	// full, not a single trailing char. (<major.minor.patch> is used rather than
	// the greedy <version>, whose own prerelease run would eat the hex.)
	p, err := pattern.Compile("<major.minor.patch>*<hex>.tgz")
	require.NoError(t, err)
	m := p.Regexp().FindStringSubmatch("tool-1.2.3-deadbeef.tgz")
	require.Equal(t, "1.2.3", m[1])
	require.Equal(t, "deadbeef", m[2])
}

func TestTokensNilForRegex(t *testing.T) {
	t.Parallel()

	p, err := pattern.Compile("/v(.*)/")
	require.NoError(t, err)
	require.Nil(t, p.Tokens(), "a /regex/ has no placeholder tokens")
}

func TestUnknownToken(t *testing.T) {
	t.Parallel()

	_, err := pattern.Compile("<bogus>")
	require.EqualError(t, err, "pattern: unknown token <bogus>")

	// A stray, unclosed angle bracket is literal, not an error.
	_, err = pattern.Compile("a < b")
	require.NoError(t, err)
}

func TestTokenBoundaries(t *testing.T) {
	t.Parallel()

	// Only the tight inner <version> is a token; the extra brackets are literal.
	p, err := pattern.Compile("<<<<<version>>>>>>")
	require.NoError(t, err)
	require.Equal(t, []string{"version"}, p.Tokens())
	require.True(t, p.Regexp().MatchString("<<<<1.2.3>>>>>"))

	// A leading or trailing dot is not a token, so it stays literal text.
	require.False(t, pattern.HasToken("<.version>"))
	require.False(t, pattern.HasToken("<version.>"))
	require.True(t, pattern.HasToken("<major.minor>"))
}

func TestExpand(t *testing.T) {
	t.Parallel()

	values := map[string]string{"version": "1.5.0", "commit": "abc123", "major.minor": "1.5"}
	require.Equal(t, "@abc123 # 1.5.0", pattern.Expand("@<commit> # <version>", values))
	require.Equal(t, "1.5", pattern.Expand("<major.minor>", values))
	require.Equal(t, "<sha256>", pattern.Expand("<sha256>", values), "unprovided tokens stay")
	require.True(t, pattern.HasToken("<sha256>"))
	require.False(t, pattern.HasToken("1.5.0"))
}
