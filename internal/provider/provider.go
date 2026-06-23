package provider

import (
	"context"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
)

// Resource is a provider-specific, validated descriptor built from a directive.
// Each provider creates and consumes its own concrete type; to the rest of clover
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

// Digester is an optional capability for providers that can resolve a version's
// content digest (e.g. an OCI image's manifest digest), for secure-pin
// rewriting. clover resolves it only for the chosen candidate of a digest-pinned
// marker.
type Digester interface {
	Digest(ctx context.Context, r Resource, tag string) (string, error)
}

// Linker is an optional capability for providers that can build a web URL for a
// resolved candidate (e.g. a GitHub release/tag page). clover hyperlinks the
// reported version when the provider implements it; absent, the value logs as
// plain text. An empty return means no meaningful URL, so the value is not
// linked.
type Linker interface {
	URL(r Resource, c model.Candidate) string
}

// Committer is an optional capability for providers that can resolve a specific
// tag's commit SHA - the peeled commit for an annotated tag. clover uses it under
// --verify to deep-check an action pin against the tag it claims, including tags
// off the discovered page or about to be bumped.
type Committer interface {
	Commit(ctx context.Context, r Resource, tag string) (string, error)
}

// Branch is a repository branch: its name and the commit at its tip.
type Branch struct {
	Name string
	Tip  string
}

// BranchChecker is an optional capability for verifying a commit's branch
// provenance under --verify: resolving the default branch, listing branches to
// match an allowed-branch pattern, and testing whether a commit is reachable
// from a branch. It guards against a tag that points at an off-trunk commit.
type BranchChecker interface {
	DefaultBranch(ctx context.Context, r Resource) (string, error)
	Branches(ctx context.Context, r Resource) ([]Branch, error)
	Reachable(ctx context.Context, r Resource, branch, commit string) (bool, error)
}

// Authenticator is an optional capability for providers that need credentials.
// clover type-asserts for it during the authenticate phase; a provider without it
// is treated as needing no authentication.
type Authenticator interface {
	// Authenticate loads and verifies credentials, without blocking on a prompt.
	Authenticate(ctx context.Context) error
	// AuthHint returns an actionable message describing how to authenticate when
	// Authenticate fails.
	AuthHint() string
}
