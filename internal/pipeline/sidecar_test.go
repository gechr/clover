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
