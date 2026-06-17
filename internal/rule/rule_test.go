package rule_test

import (
	"testing"

	"github.com/gechr/cusp/internal/directive"
	"github.com/gechr/cusp/internal/rule"
	"github.com/gechr/cusp/internal/version"
	"github.com/stretchr/testify/require"
)

type cand struct{ tag string }

func attrsOf(c cand) version.Attrs {
	v, _ := version.Parse(c.tag)
	return version.Attrs{Tag: c.tag, Semver: v}
}

func directiveOf(pairs ...directive.KV) directive.Directive {
	return directive.Directive{Pairs: pairs}
}

func candidates(tags ...string) []cand {
	cands := make([]cand, len(tags))
	for i, tag := range tags {
		cands[i] = cand{tag: tag}
	}
	return cands
}

// selected compiles d's rule, runs version.Select, and returns the chosen tag.
func selected(
	t *testing.T,
	d directive.Directive,
	current *version.Version,
	cands []cand,
) (string, bool) {
	t.Helper()
	opts, err := rule.Compile(d, current)
	require.NoError(t, err)
	got, ok := version.Select(current, cands, attrsOf, opts...)
	return got.tag, ok
}

func TestCompileConstraint(t *testing.T) {
	t.Parallel()

	current := mustParse(t, "1.2.0")
	d := directiveOf(directive.KV{Key: "constraint", Value: "minor"})

	tag, ok := selected(t, d, current, candidates("1.2.0", "1.5.0", "2.0.0"))
	require.True(t, ok)
	require.Equal(t, "1.5.0", tag, "major bump excluded by minor ceiling")
}

func TestCompileIncludeExclude(t *testing.T) {
	t.Parallel()

	cands := candidates("1.2.0", "1.3.0", "1.4.0")

	tag, ok := selected(t, directiveOf(directive.KV{Key: "include", Value: "1.3*"}), nil, cands)
	require.True(t, ok)
	require.Equal(t, "1.3.0", tag)

	tag, ok = selected(t, directiveOf(directive.KV{Key: "exclude", Value: "1.4*"}), nil, cands)
	require.True(t, ok)
	require.Equal(t, "1.3.0", tag)
}

func TestCompileBehind(t *testing.T) {
	t.Parallel()

	tag, ok := selected(t, directiveOf(directive.KV{Key: "behind", Value: "1"}), nil,
		candidates("1.2.0", "1.3.0", "1.4.0"))
	require.True(t, ok)
	require.Equal(t, "1.3.0", tag)
}

func TestCompilePrerelease(t *testing.T) {
	t.Parallel()

	cands := candidates("1.2.0", "1.3.0-rc.1")

	tag, ok := selected(t, directiveOf(), nil, cands)
	require.True(t, ok)
	require.Equal(t, "1.2.0", tag, "prereleases excluded by default")

	tag, ok = selected(t, directiveOf(directive.KV{Key: "prerelease", Value: "true"}), nil, cands)
	require.True(t, ok)
	require.Equal(t, "1.3.0-rc.1", tag)
}

func TestCompileErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		d    directive.Directive
	}{
		{
			name: "bad constraint",
			d:    directiveOf(directive.KV{Key: "constraint", Value: "not-a-range"}),
		},
		{name: "bad regex include", d: directiveOf(directive.KV{Key: "include", Value: "/(/"})},
		{name: "non-integer behind", d: directiveOf(directive.KV{Key: "behind", Value: "two"})},
		{name: "negative behind", d: directiveOf(directive.KV{Key: "behind", Value: "-1"})},
		{name: "bad prerelease", d: directiveOf(directive.KV{Key: "prerelease", Value: "yes"})},
		{name: "bad cooldown", d: directiveOf(directive.KV{Key: "cooldown", Value: "soon"})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := rule.Compile(tt.d, nil)
			require.Error(t, err)
		})
	}
}

func mustParse(t *testing.T, s string) *version.Version {
	t.Helper()
	v, err := version.Parse(s)
	require.NoError(t, err)
	return v
}
