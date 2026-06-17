package provider

import (
	"context"

	"github.com/gechr/cusp/internal/directive"
	"github.com/gechr/cusp/internal/model"
)

// Resource is a provider-specific, validated descriptor built from a directive.
// Each provider creates and consumes its own concrete type; to the rest of cusp
// it is opaque, so a single registry can hold heterogeneous providers.
type Resource any

// Key is one directive key a provider accepts. Keys are returned in the
// provider's canonical order and drive both validation (required keys) and the
// middle zone of the directive's canonical key ordering in format mode.
type Key struct {
	Name     string
	Required bool
}

// Provider adapts one upstream source of versions. Resource validates a
// directive into a descriptor once; Discover is the single network method,
// returning rich candidates with whatever metadata the API gave for free.
type Provider interface {
	// Name is the provider's identifier, as written in a directive's provider=.
	Name() string
	// Keys reports the directive keys the provider accepts, in canonical order.
	Keys() []Key
	// Resource builds and validates the provider's descriptor from a directive,
	// erroring on missing required keys.
	Resource(d directive.Directive) (Resource, error)
	// Describe returns a human-readable label for a resource.
	Describe(r Resource) string
	// Discover lists the candidate versions for a resource.
	Discover(ctx context.Context, r Resource) ([]model.Candidate, error)
}

// Authenticator is an optional capability for providers that need credentials.
// cusp type-asserts for it during the authenticate phase; a provider without it
// is treated as needing no authentication.
type Authenticator interface {
	// Authenticate loads and verifies credentials, without blocking on a prompt.
	Authenticate(ctx context.Context) error
	// AuthHint returns an actionable message describing how to authenticate when
	// Authenticate fails.
	AuthHint() string
}
