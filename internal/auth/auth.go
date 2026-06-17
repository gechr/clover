// Package auth reports credential status for the providers a run uses. It asks
// each provider that supports authentication (the Authenticator capability)
// whether a credential is available, so the CLI can warn - actionably - before
// falling back to anonymous, rate-limited access. It performs no network calls
// beyond each provider's local credential lookup.
package auth

import (
	"context"

	"github.com/gechr/clover/internal/provider"
)

// Status is the credential state of one provider.
type Status struct {
	Provider      string
	Authenticated bool
	Hint          string // how to authenticate, set only when not authenticated
}

// Check resolves credential status for each named provider that supports
// authentication. Names not registered, or providers that need no credentials,
// are skipped, so the result holds only providers with something to report.
func Check(ctx context.Context, names []string) []Status {
	statuses := make([]Status, 0, len(names))
	for _, name := range names {
		prov, ok := provider.Get(name)
		if !ok {
			continue
		}
		authenticator, ok := prov.(provider.Authenticator)
		if !ok {
			continue // provider needs no credentials
		}

		status := Status{Provider: name, Authenticated: true}
		if err := authenticator.Authenticate(ctx); err != nil {
			status.Authenticated = false
			status.Hint = authenticator.AuthHint()
		}
		statuses = append(statuses, status)
	}
	return statuses
}
