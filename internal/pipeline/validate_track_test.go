package pipeline_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// resourceErrProvider is a registered provider whose Resource always errors, so
// checkTrack's resource branch is reachable.
type resourceErrProvider struct{ name string }

func (p resourceErrProvider) Name() string { return p.name }

func (p resourceErrProvider) Keys() []provider.Key              { return []provider.Key{{Name: "repository"}} }
func (p resourceErrProvider) Describe(provider.Resource) string { return p.name }

func (p resourceErrProvider) Resource(directive.Directive) (provider.Resource, error) {
	return nil, errors.New("resource is broken")
}

func (p resourceErrProvider) Discover(
	context.Context,
	provider.Resource,
) ([]model.Candidate, error) {
	return nil, nil
}

func TestValidateTrackUnknownProvider(t *testing.T) {
	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=nosuchtrack repository=x/y track=stable\n" +
			"FROM x/y:latest\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Error(t, files[0].Results[0].Err, "an unknown provider fails the track check")
}

func TestValidateTrackResourceError(t *testing.T) {
	provider.Register(resourceErrProvider{name: "brokentrack"})

	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=brokentrack repository=x/y track=stable\n" +
			"FROM x/y:latest\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.EqualError(t, files[0].Results[0].Err, "resource is broken")
}

func TestValidateTrackLocateFails(t *testing.T) {
	provider.Register(fakeProvider{name: "docker"})

	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=x/y track=*\n" +
			"plain text, no image pin here\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Error(t, files[0].Results[0].Err, "a line the track rewriter cannot locate fails")
}

func TestValidateTrackCapableDigest(t *testing.T) {
	newDigest := "sha256:" + strings.Repeat("b", 64)
	provider.Register(fakeProvider{name: "docker", digest: newDigest})

	oldDigest := "sha256:" + strings.Repeat("a", 64)
	dir := write(t, map[string]string{
		"Dockerfile": "# clover: provider=docker repository=x/y track=*\n" +
			"FROM x/y:latest@" + oldDigest + "\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.NoError(t, files[0].Results[0].Err,
		"a Digester provider satisfies a tracked digest pin")
}

func TestValidateTrackCapableCommit(t *testing.T) {
	const commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	provider.Register(fakeProvider{
		name:      "github",
		tagCommit: map[string]string{"main": commit},
	})

	dir := write(t, map[string]string{
		".github/workflows/ci.yml": "# clover: provider=github repository=x/y track=main\n" +
			"  - uses: x/y@" + commit + " # main\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.NoError(t, files[0].Results[0].Err,
		"a Committer provider satisfies a tracked branch pin")
}
