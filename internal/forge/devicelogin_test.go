package forge_test

import (
	"testing"

	"github.com/gechr/clover/internal/forge"
	"github.com/stretchr/testify/require"
)

func TestDeviceLoginInvalidHost(t *testing.T) {
	t.Parallel()

	cfg := forge.DeviceConfig{
		Label:       "gitea",
		DefaultHost: "codeberg.org",
	}
	err := forge.DeviceLogin(
		t.Context(),
		cfg,
		"git.example.com/path", // a path is not a valid authority
		"",
		func(forge.Code) { t.Fatal("prompt must not run for an invalid host") },
	)
	require.EqualError(t, err, "gitea: invalid host")
}

func TestDeviceLoginPrivateHostRequiresClientID(t *testing.T) {
	t.Parallel()

	cfg := forge.DeviceConfig{
		Label:           "gitea",
		DefaultHost:     "codeberg.org",
		DefaultClientID: "public-app",
		ClientIDHint:    "register an OAuth app",
	}
	err := forge.DeviceLogin(
		t.Context(),
		cfg,
		"git.private.example", // a private instance embeds no app
		"",
		func(forge.Code) { t.Fatal("prompt must not run before the client ID guard") },
	)
	require.EqualError(
		t, err,
		"gitea: git.private.example requires --client-id (register an OAuth app)",
	)
}
