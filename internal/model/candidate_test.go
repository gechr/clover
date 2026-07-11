package model_test

import (
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

func TestNewCandidate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		raw      string
		semver   bool // whether Semver is expected non-nil
		semverIs string
	}{
		"semver tag":     {raw: "v1.27.0", semver: true, semverIs: "1.27.0"},
		"non-semver ref": {raw: "nightly", semver: false},
		"empty":          {raw: "", semver: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := model.NewCandidate(tt.raw)
			require.Equal(t, tt.raw, c.Version)
			require.Equal(t, tt.raw, c.Ref)
			if tt.semver {
				require.NotNil(t, c.Semver)
				require.Equal(t, tt.semverIs, c.Semver.String())
			} else {
				require.Nil(t, c.Semver)
			}
		})
	}
}

func TestNewVariantCandidate(t *testing.T) {
	t.Parallel()

	// A variant suffix is stripped before parsing, so the tag orders by its
	// numeric core rather than as a prerelease.
	alpine := model.NewVariantCandidate("1.27-alpine")
	require.Equal(t, "1.27-alpine", alpine.Version, "Version keeps the full raw tag")
	require.Equal(t, "1.27-alpine", alpine.Ref)
	require.NotNil(t, alpine.Semver)
	require.Empty(t, alpine.Semver.Prerelease(), "the alpine variant is not a prerelease")

	// Parsing the full raw tag instead treats -alpine as a prerelease, so the
	// variant-aware parse yields a different semver.
	require.NotEqual(t, model.NewCandidate("1.27-alpine").Semver, alpine.Semver,
		"stripping the variant changes the parsed semver")

	// A true prerelease is kept, not stripped.
	rc := model.NewVariantCandidate("2.0.0-rc.1")
	require.Equal(t, "2.0.0-rc.1", rc.Version)
	require.NotNil(t, rc.Semver)
	require.Equal(t, "rc.1", rc.Semver.Prerelease(), "a real prerelease survives")

	// A bare version parses the same as NewCandidate.
	require.Equal(t, model.NewCandidate("1.27").Semver, model.NewVariantCandidate("1.27").Semver)
}
