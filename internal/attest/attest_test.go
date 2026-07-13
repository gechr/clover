package attest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gechr/clover/internal/attest"
	"github.com/sigstore/sigstore-go/pkg/testing/data"
	"github.com/stretchr/testify/require"
)

const (
	fixtureDigest = "sha512:46d4e2f74c4877316640000a6fdf8a8b59f1e0847667973e9859f774dd31b8f1e0937813b777fb66a2ac67d50540fe34640966eee9fc2ccca387082b4c85cd3c"
	fixtureSAN    = "https://github.com/sigstore/sigstore-js/.github/workflows/release.yml@refs/heads/main"
)

func TestVerify(t *testing.T) {
	t.Parallel()

	entity := data.Bundle(t, "sigstore.js@2.0.0-provenance.sigstore.json")
	contents, err := json.Marshal(entity)
	require.NoError(t, err)
	v := attest.New(attest.WithTrustedMaterial(data.TrustedRoot(t, "public-good.json")))

	tests := []struct {
		name     string
		bundles  [][]byte
		digest   string
		identity string
		issuer   string
		want     bool
	}{
		{
			name: "matching glob", bundles: [][]byte{contents}, digest: fixtureDigest,
			identity: "https://github.com/sigstore/sigstore-js/.github/workflows/*", want: true,
		},
		{
			name: "matching regex", bundles: [][]byte{contents}, digest: fixtureDigest,
			identity: `/.*sigstore-js\/.github\/workflows\/release\.yml@.*/`, want: true,
		},
		{
			// A regex is anchored to the whole SAN: a bare substring must not
			// match, or an attacker SAN containing it would pass verification.
			name: "regex is whole string", bundles: [][]byte{contents}, digest: fixtureDigest,
			identity: `/sigstore-js\/.github\/workflows\/release\.yml/`, want: false,
		},
		{
			name: "identity mismatch", bundles: [][]byte{contents}, digest: fixtureDigest,
			identity: "https://github.com/example/*", want: false,
		},
		{
			name: "glob is whole string", bundles: [][]byte{contents}, digest: fixtureDigest,
			identity: "https://github.com/sigstore/sigstore-js", want: false,
		},
		{
			name: "issuer mismatch", bundles: [][]byte{contents}, digest: fixtureDigest,
			identity: fixtureSAN, issuer: "https://issuer.example.com", want: false,
		},
		{name: "zero bundles", digest: fixtureDigest, identity: fixtureSAN, want: false},
		{
			name: "malformed before valid", bundles: [][]byte{[]byte(`{`), contents},
			digest: fixtureDigest, identity: fixtureSAN, want: true,
		},
		{
			name:     "wrong digest",
			bundles:  [][]byte{contents},
			digest:   "sha512:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			identity: fixtureSAN,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := v.Verify(
				context.Background(),
				tt.bundles,
				tt.digest,
				tt.identity,
				tt.issuer,
			)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
