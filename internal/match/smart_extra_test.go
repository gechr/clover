package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

func TestSmartLocated_Rendered(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line      string
		candidate string
		want      string
	}{
		"v-prefix preserved":  {line: "v1.2.3", candidate: "1.3.0", want: "v1.3.0"},
		"no prefix":           {line: "1.2.3", candidate: "1.3.0", want: "1.3.0"},
		"precision preserved": {line: "1.2", candidate: "1.3.0", want: "1.3"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			loc, err := match.NewSmart().Locate(tt.line)
			require.NoError(t, err)

			renderer, ok := loc.(match.Renderer)
			require.True(t, ok, "a smart location reports its rendered text")

			got := renderer.Rendered(model.Candidate{Version: tt.candidate})
			require.Equal(t, tt.want, got)
		})
	}
}
