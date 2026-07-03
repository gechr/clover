package github_test

import (
	"testing"

	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/stretchr/testify/require"
)

// TestLoginEnterpriseRequiresClientID covers the enterprise guard: a GitHub
// Enterprise Server host has no embeddable clover app, so login refuses without
// an explicit --client-id rather than attempting the flow with github.com's app.
func TestLoginEnterpriseRequiresClientID(t *testing.T) {
	t.Parallel()

	err := github.Login(t.Context(), "ghe.example.com", "", func(forge.Code) {})
	require.EqualError(t, err,
		"github: ghe.example.com requires --client-id (register an OAuth app on the instance)")
}

// TestLoginInvalidHost covers a malformed --host: it is rejected before any
// network call.
func TestLoginInvalidHost(t *testing.T) {
	t.Parallel()

	err := github.Login(t.Context(), "ghe.example.com/foo", "id", func(forge.Code) {})
	require.EqualError(t, err, "github: invalid host")
}
