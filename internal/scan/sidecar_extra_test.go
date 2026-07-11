package scan_test

import (
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/scan"
	"github.com/stretchr/testify/require"
)

func TestIsSidecar(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path string
		want bool
	}{
		"bare sidecar yaml": {path: "config.clover.yaml", want: true},
		"bare sidecar yml":  {path: "config.clover.yml", want: true},
		"nested sidecar":    {path: filepath.Join("dir", "app.clover.yaml"), want: true},
		"plain file":        {path: "main.go", want: false},
		"bare config":       {path: ".clover.yaml", want: false},
		"empty":             {path: "", want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, scan.IsSidecar(tt.path))
		})
	}
}
