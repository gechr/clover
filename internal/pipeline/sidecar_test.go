package pipeline_test

import (
	"context"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// A strict-JSON target carries no inline clover: comment; its sidecar's find
// locator picks the $schema line, and the find/replace rewriter bumps only the
// version token, leaving every other byte of the JSON intact.
func TestRunSidecarFindRewritesTarget(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "sidecarfake",
		candidates: []model.Candidate{candidate(t, "1.5.3"), candidate(t, "1.8.0")},
	})

	const tsconfig = `{
  "$schema": "https://biomejs.dev/schemas/1.5.3/schema.json",
  "compilerOptions": { "strict": true }
}
`
	dir := write(t, map[string]string{
		"tsconfig.json": tsconfig,
		"tsconfig.json.clover.yaml": "- provider: sidecarfake\n" +
			"  repository: biomejs/biome\n" +
			"  find: schemas/<version>/schema.json\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Len(t, files[0].Results, 1)

	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.True(t, r.Changed)
	require.Equal(t, 1, r.Marker.Target, "the $schema line, resolved by find")
	require.Equal(t,
		`  "$schema": "https://biomejs.dev/schemas/1.8.0/schema.json",`,
		r.NewLine,
	)

	// Golden: applying the rewrite bumps only the version; every other byte is
	// identical to the original JSON.
	want := strings.Replace(tsconfig, "schemas/1.5.3/schema.json", "schemas/1.8.0/schema.json", 1)
	lines := strings.Split(tsconfig, "\n")
	lines[r.Marker.Target] = r.NewLine
	require.Equal(t, want, strings.Join(lines, "\n"))
}

// A jq locator with no find targets the $schema line by path, and the smart
// rewriter shape-matches the single version token in the URL - so jq works on a
// strict JSON value even though no version key is named.
func TestRunSidecarJQRewritesTarget(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "sidecarjqfake",
		candidates: []model.Candidate{candidate(t, "1.5.3"), candidate(t, "1.8.0")},
	})

	const tsconfig = `{
  "$schema": "https://biomejs.dev/schemas/1.5.3/schema.json",
  "compilerOptions": { "strict": true }
}
`
	dir := write(t, map[string]string{
		"tsconfig.json": tsconfig,
		"tsconfig.json.clover.yaml": "- provider: sidecarjqfake\n" +
			"  repository: biomejs/biome\n" +
			"  jq: '.[\"$schema\"]'\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)

	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.True(t, r.Changed)
	require.Equal(t, 1, r.Marker.Target, "the $schema line, resolved by jq path")
	require.Equal(t,
		`  "$schema": "https://biomejs.dev/schemas/1.8.0/schema.json",`,
		r.NewLine,
	)

	// Golden: key order preserved and only the version changed - proof the JSON
	// was never re-serialized.
	want := strings.Replace(tsconfig, "schemas/1.5.3/schema.json", "schemas/1.8.0/schema.json", 1)
	lines := strings.Split(tsconfig, "\n")
	lines[r.Marker.Target] = r.NewLine
	require.Equal(t, want, strings.Join(lines, "\n"))
}

// jq + find compose, and demonstrate why jq matters: the same version string
// appears on two lines, so a find alone is ambiguous, but jq selects the $schema
// line and find then refines to the version token within that line.
func TestRunSidecarJQFindComposes(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "sidecarjqfindfake",
		candidates: []model.Candidate{candidate(t, "1.5.3"), candidate(t, "1.8.0")},
	})

	const tsconfig = `{
  "$schema": "https://biomejs.dev/schemas/1.5.3/schema.json",
  "version": "1.5.3"
}
`
	dir := write(t, map[string]string{
		"tsconfig.json": tsconfig,
		"tsconfig.json.clover.yaml": "- provider: sidecarjqfindfake\n" +
			"  repository: biomejs/biome\n" +
			"  jq: '.[\"$schema\"]'\n" +
			"  find: <version>\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)

	r := files[0].Results[0]
	require.NoError(t, r.Err)
	require.True(t, r.Changed)
	require.Equal(
		t,
		1,
		r.Marker.Target,
		"jq disambiguates to the $schema line, not the version line",
	)
	require.Equal(t,
		`  "$schema": "https://biomejs.dev/schemas/1.8.0/schema.json",`,
		r.NewLine,
	)
	require.Equal(
		t,
		`  "version": "1.5.3"`,
		files[0].Lines[2],
		"the matching version line is untouched",
	)
}
