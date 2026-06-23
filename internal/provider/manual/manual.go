package manual

import (
	"context"
	"fmt"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// Provider is a human-owned root. It resolves to the value already on its target
// line and publishes it under the marker's id for followers, contacting no
// upstream. There is no selection stage - the value changes only when a person
// edits the line - so clover leaves the line untouched and never bumps it.
type Provider struct{}

// New returns the manual provider.
func New() *Provider { return &Provider{} }

// Name identifies the provider.
func (p *Provider) Name() string { return constant.ProviderManual }

// Anchor marks the provider as line-anchored: clover reads its value from the
// target line and skips discovery and selection.
func (p *Provider) Anchor() {}

// Keys reports the directive keys manual accepts. It declares none of its own -
// a manual root is configured entirely with the common vocabulary (id to publish
// the value, find to pin which token on an ambiguous line is the value).
func (p *Provider) Keys() []provider.Key { return nil }

// Resource validates the directive. A manual root is pointless without an id to
// publish under - it neither rewrites its line nor contacts an upstream - so the
// id is required; nothing else is needed from an upstream.
func (p *Provider) Resource(d directive.Directive) (provider.Resource, error) {
	if id, ok := d.Get(constant.DirectiveID); !ok || id == "" {
		return nil, fmt.Errorf("manual: %s is required", constant.DirectiveID)
	}
	return resource{}, nil
}

// Describe returns a human-readable label for a resource.
func (p *Provider) Describe(provider.Resource) string { return constant.ProviderManual }

// Discover is never called: clover short-circuits an anchored provider before
// discovery, resolving the value from the target line instead.
func (p *Provider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	return nil, nil
}

// resource is the manual provider's opaque descriptor. It carries nothing: the
// resolved value comes from the located line, not the directive.
type resource struct{}
