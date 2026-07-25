package match

import (
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/pattern"
	"github.com/stretchr/testify/require"
)

func TestYAMLScalar(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   string
		want string
	}{
		"bare":                  {in: "nginx:1.27", want: "nginx:1.27"},
		"leading space":         {in: "  nginx:1.27", want: "nginx:1.27"},
		"bare inline comment":   {in: "nginx:1.27 # pinned", want: "nginx:1.27"},
		"double quoted":         {in: `"nginx:1.27"`, want: "nginx:1.27"},
		"double quoted comment": {in: `"nginx:1.27" # pinned`, want: "nginx:1.27"},
		"single quoted":         {in: `'nginx:1.27'`, want: "nginx:1.27"},
		"single quoted comment": {in: `'nginx:1.27' # pinned`, want: "nginx:1.27"},
		// A \" is an escaped quote, not the close, so the real closing quote is found.
		"double escaped quote": {in: `"a\"b"`, want: `a"b`},
		"double escaped slash": {in: `"a\\b"`, want: `a\b`},
		// '' is YAML's escape for a literal single quote.
		"single doubled quote": {in: `'it''s'`, want: "it's"},
		// An unterminated quote falls back to the rest of the value.
		"unterminated double": {in: `"nginx:1.27`, want: "nginx:1.27"},
		"unterminated single": {in: `'nginx:1.27`, want: "nginx:1.27"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, yamlScalar(tc.in))
		})
	}
}

func TestAlternation(t *testing.T) {
	t.Parallel()

	require.Equal(t, "go|node", alternation([]string{"go", "node"}))
	require.Equal(
		t,
		`dotnet\.exe|xxh`,
		alternation([]string{"dotnet.exe", "xxh"}),
		"meta chars are quoted",
	)

	// An empty name list yields a group that matches nothing, so a route built
	// from an empty generated map cannot collapse to `()` and claim stray lines.
	require.Equal(t, `[^\s\S]`, alternation(nil))
	p, err := pattern.Compile(`/^\s*"?(` + alternation(nil) + `)"?\s*=\s*"/`)
	require.NoError(t, err)
	require.False(t, p.Matches(`foo = "1.2.3"`))
	require.False(t, p.Matches(` = "1.2.3"`))
}

// TestRouteInference is the drift guard for the deliberate redundancy between a
// route's provider guard and its inference. The guard
// serves rewriter dispatch (an explicit provider picks the rewriter) and the
// inference serves auto-detection, so the two are separate fields that must
// nonetheless always agree: a route that dispatched one provider and inferred
// another would resolve a marker against an upstream the line never named.
//
// init already asserts a dispatching route carries an inference; this pins the
// value. A provider-fixed inference names its provider unconditionally, so an
// empty line is enough to read it back without depending on a shape.
//
// A detection-only route is the deliberate exception: it declares no provider
// because it reads one from the file, so an empty line must leave it declining
// rather than defaulting to some provider.
func TestRouteInference(t *testing.T) {
	t.Parallel()

	empty := subject{lines: []string{""}, target: 0}
	detectionOnly := 0
	for _, r := range routes {
		if r.infer == nil {
			continue
		}
		if r.when.provider == "" {
			detectionOnly++
			require.Empty(t, r.infer(empty).Provider,
				"a detection-only route must read its provider from the file")
			continue
		}
		require.Equal(t, r.when.provider, r.infer(empty).Provider,
			"route %q infers a different provider than it dispatches", r.when.provider)
	}
	require.NotZero(t, detectionOnly, "the detection-only branch is unexercised")
}

// TestToolNameSetsDisjoint enforces what two doc comments assert and nothing
// checked: the four tool-name sets feeding the mise route alternations name
// disjoint tools. They are separate routes in a fixed order, so a name in two
// sets resolves to whichever route is written first - silently, and against the
// wrong upstream for every user of that tool.
//
// The sets are not all generated together: miseGithubTools is hand-curated and
// unioned into ToolNames, so one hand-added entry colliding with a regenerated
// ecosystem map is the failure this catches, in the standard gate rather than at
// the next `go generate`.
func TestToolNameSetsDisjoint(t *testing.T) {
	t.Parallel()

	sets := map[string][]string{
		constant.ProviderGithub: ToolNames(),
		constant.ProviderPypi:   pypiToolNames(),
		constant.ProviderNpm:    npmToolNames(),
		constant.ProviderCrates: cratesToolNames(),
	}

	owner := map[string]string{}
	for set, names := range sets {
		for _, name := range names {
			previous, duplicated := owner[name]
			require.False(t, duplicated,
				"tool %q is in both the %s and %s sets, so whichever route comes "+
					"first claims it", name, previous, set)
			owner[name] = set
		}
	}

	// A tool a native provider resolves must not also be an ecosystem package:
	// the routes and toolInference both put the native provider first, so the
	// ecosystem entry would be permanently unreachable rather than merely
	// shadowed.
	for name := range nativeToolProviders {
		if set := owner[name]; set != "" && set != constant.ProviderGithub {
			t.Errorf("native tool %q is also in the %s set", name, set)
		}
	}
	// The one documented overlap: rust keeps a github mapping so an explicit
	// provider=github tool=rust still resolves, while every route and inference
	// reaches the native provider first. Pinned so the next overlap is a choice.
	var nativeOnGithub []string
	for name := range nativeToolProviders {
		if owner[name] == constant.ProviderGithub {
			nativeOnGithub = append(nativeOnGithub, name)
		}
	}
	require.Equal(t, []string{"rust"}, nativeOnGithub,
		"a native tool in the github set must be the documented rust exception")

	// A HashiCorp product is routed by its own mise route, which precedes them
	// all; an entry in any generated set would be dead and misleading.
	for _, product := range hashicorpProducts {
		require.Empty(t, owner[product],
			"HashiCorp product %q is also in the %s set", product, owner[product])
	}
}
