package gitlab_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/stretchr/testify/require"
)

// TestLoginSelfManagedRequiresClientID covers the self-managed guard: such an
// instance runs its own OAuth application, so login refuses without an explicit
// --client-id rather than attempting the flow with gitlab.com's app.
func TestLoginSelfManagedRequiresClientID(t *testing.T) {
	t.Parallel()

	err := gitlab.Login(t.Context(), "gitlab.example.com", "", func(gitlab.Code) {})
	require.EqualError(t, err,
		"gitlab: gitlab.example.com requires --client-id (register an application on the instance)")
}

// TestLoginInvalidHost covers a malformed --host: it is rejected before any
// network call.
func TestLoginInvalidHost(t *testing.T) {
	t.Parallel()

	err := gitlab.Login(t.Context(), "gitlab.example.com/foo", "id", func(gitlab.Code) {})
	require.EqualError(t, err, "gitlab: invalid host")
}
