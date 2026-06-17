package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gechr/clover/internal/auth"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// authProvider is a registered provider with the Authenticator capability; its
// Authenticate result is fixed by the test.
type authProvider struct {
	name string
	err  error
}

func (p authProvider) Name() string         { return p.name }
func (p authProvider) Keys() []provider.Key { return nil }
func (p authProvider) Resource(directive.Directive) (provider.Resource, error) {
	return p.name, nil
}
func (p authProvider) Describe(provider.Resource) string { return p.name }

func (p authProvider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	return nil, nil
}

func (p authProvider) Authenticate(context.Context) error { return p.err }
func (p authProvider) AuthHint() string                   { return "do the auth thing" }

// plainProvider implements only Provider, not Authenticator - it needs no
// credentials.
type plainProvider struct{ name string }

func (p plainProvider) Name() string                      { return p.name }
func (p plainProvider) Keys() []provider.Key              { return nil }
func (p plainProvider) Describe(provider.Resource) string { return p.name }
func (p plainProvider) Resource(directive.Directive) (provider.Resource, error) {
	return p.name, nil
}

func (p plainProvider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	return nil, nil
}

func TestCheck(t *testing.T) {
	provider.Register(authProvider{name: "authed"})
	provider.Register(authProvider{name: "anon", err: errors.New("no creds")})
	provider.Register(plainProvider{name: "plain"})

	got := auth.Check(context.Background(), []string{"authed", "anon", "plain", "missing"})

	require.Equal(t, []auth.Status{
		{Provider: "authed", Authenticated: true},
		{Provider: "anon", Authenticated: false, Hint: "do the auth thing"},
	}, got, "non-authenticators and unregistered names are skipped; hint set only when anonymous")
}

func TestCheckEmpty(t *testing.T) {
	require.Empty(t, auth.Check(context.Background(), nil))
}
