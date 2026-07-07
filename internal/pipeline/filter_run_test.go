package pipeline_test

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// --enable resolves only the named providers; other markers drop out of the run
// entirely rather than resolving.
func TestRunProviderFilterEnable(t *testing.T) {
	provider.Register(
		fakeProvider{name: "pfgithub", candidates: []model.Candidate{candidate(t, "1.3.0")}},
	)
	provider.Register(
		fakeProvider{name: "pfdocker", candidates: []model.Candidate{candidate(t, "2.0.0")}},
	)

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=pfgithub repository=x/y\nver: 1.2.0\n" +
			"# clover: provider=pfdocker repository=x/y\nimg: 1.9.0\n",
	})

	enable, err := provider.NewFilter([]string{"pfgithub"}, nil)
	require.NoError(t, err)

	files, err := pipeline.Run(
		context.Background(),
		[]string{dir},
		pipeline.WithProviderFilter(enable),
	)
	require.NoError(t, err)
	require.Len(t, files[0].Results, 1, "only the pfgithub marker survives")
	require.Equal(t, "ver: 1.3.0", files[0].Results[0].NewLine)
}

// --disable resolves everything but the named providers.
func TestRunProviderFilterDisable(t *testing.T) {
	provider.Register(
		fakeProvider{name: "pfgithub", candidates: []model.Candidate{candidate(t, "1.3.0")}},
	)
	provider.Register(
		fakeProvider{name: "pfdocker", candidates: []model.Candidate{candidate(t, "2.0.0")}},
	)

	dir := write(t, map[string]string{
		"app.txt": "# clover: provider=pfgithub repository=x/y\nver: 1.2.0\n" +
			"# clover: provider=pfdocker repository=x/y\nimg: 1.9.0\n",
	})

	disable, err := provider.NewFilter(nil, []string{"pfdocker"})
	require.NoError(t, err)

	files, err := pipeline.Run(
		context.Background(),
		[]string{dir},
		pipeline.WithProviderFilter(disable),
	)
	require.NoError(t, err)
	require.Len(t, files[0].Results, 1, "the pfdocker marker is skipped")
	require.Equal(t, "ver: 1.3.0", files[0].Results[0].NewLine)
}
