package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/match"
	"github.com/stretchr/testify/require"
)

func TestInference_Missing(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   match.Inference
		want string
	}{
		"docker no repository": {
			in:   match.Inference{Provider: constant.ProviderDocker},
			want: "reference has no repository",
		},
		"docker complete": {
			in:   match.Inference{Provider: constant.ProviderDocker, Repository: "nginx"},
			want: "",
		},
		"github no repository": {
			in:   match.Inference{Provider: constant.ProviderGithub},
			want: "reference has no repository",
		},
		"github complete": {
			in:   match.Inference{Provider: constant.ProviderGithub, Repository: "owner/repo"},
			want: "",
		},
		"gitlab no repository": {
			in:   match.Inference{Provider: constant.ProviderGitlab},
			want: "reference has no repository",
		},
		"gitlab complete": {
			in:   match.Inference{Provider: constant.ProviderGitlab, Repository: "org/proj"},
			want: "",
		},
		"hashicorp no product": {
			in:   match.Inference{Provider: constant.ProviderHashicorp},
			want: "line names no product",
		},
		"hashicorp complete": {
			in:   match.Inference{Provider: constant.ProviderHashicorp, Product: "terraform"},
			want: "",
		},
		"terraform no source": {
			in:   match.Inference{Provider: constant.ProviderTerraform},
			want: "block names no source",
		},
		"terraform complete": {
			in:   match.Inference{Provider: constant.ProviderTerraform, Source: "hashicorp/aws"},
			want: "",
		},
		"opentofu no source": {
			in:   match.Inference{Provider: constant.ProviderOpentofu},
			want: "block names no source",
		},
		"opentofu complete": {
			in:   match.Inference{Provider: constant.ProviderOpentofu, Source: "hashicorp/aws"},
			want: "",
		},
		"unlisted provider": {
			in:   match.Inference{Provider: constant.ProviderNode},
			want: "",
		},
		"empty inference": {
			in:   match.Inference{},
			want: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.in.Missing())
		})
	}
}
