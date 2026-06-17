package display_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/display"
	"github.com/stretchr/testify/require"
)

func TestValue(t *testing.T) {
	t.Parallel()

	const (
		commit = "0123456789abcdef0123456789abcdef01234567"                         // 40 hex
		sha256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" // 64 hex
	)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "version untouched", in: "v1.27.0", want: "v1.27.0"},
		{name: "calver untouched", in: "2024.01-alpine", want: "2024.01-alpine"},
		{name: "empty untouched", in: "", want: ""},
		{name: "commit abbreviated", in: commit, want: "012345…234567"},
		{name: "sha256 abbreviated", in: sha256, want: "e3b0c4…52b855"},
		{name: "short hex untouched", in: "abc123", want: "abc123"},
		{
			name: "non-hex 40 chars untouched",
			in:   strings.Repeat("g", 40),
			want: strings.Repeat("g", 40),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := display.Value(tc.in)
			require.Equal(t, tc.want, got)
			if tc.in != tc.want {
				require.LessOrEqual(
					t,
					len([]rune(got)),
					len([]rune(tc.in)),
					"abbreviated value is no longer than the input",
				)
			}
		})
	}
}

func TestIsHash(t *testing.T) {
	t.Parallel()

	require.True(
		t,
		display.IsHash("0123456789abcdef0123456789abcdef01234567"),
		"40 hex is a commit",
	)
	require.True(
		t,
		display.IsHash("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"),
		"64 hex is a sha256",
	)
	require.False(t, display.IsHash("v1.2.3"), "version is not a hash")
	require.False(t, display.IsHash(strings.Repeat("z", 40)), "non-hex is not a hash")
	require.False(t, display.IsHash("0123456789abcdef"), "16 hex is the wrong length")
}
