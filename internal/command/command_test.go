package command_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestDefaultConstraint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		want    string
	}{
		{name: "clean release", current: "v1.2.3", want: ">=1.2.3"},
		{name: "no v prefix", current: "1.2.3", want: ">=1.2.3"},
		{name: "dev prerelease yields none", current: "v0.0.0-gabcdef1-dev", want: ""},
		{name: "unparseable yields none", current: "(devel)", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, command.DefaultConstraint(tc.current))
		})
	}
}

func TestValidateConstraint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{name: "blank is allowed", expr: ""},
		{name: "range", expr: ">=0.1.0"},
		{name: "tilde range", expr: "~>0.1"},
		{name: "keyword", expr: "minor"},
		{name: "garbage rejected", expr: "not a constraint!!", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := command.ValidateConstraint("1.2.3", tc.expr)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// authedProvider implements the Authenticator capability with a fixed result.
type authedProvider struct {
	name string
	err  error
}

func (p authedProvider) Name() string                      { return p.name }
func (p authedProvider) Keys() []provider.Key              { return nil }
func (p authedProvider) Describe(provider.Resource) string { return p.name }
func (p authedProvider) Resource(directive.Directive) (provider.Resource, error) {
	return p.name, nil
}

func (p authedProvider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	return nil, nil
}
func (p authedProvider) Authenticate(context.Context) error { return p.err }
func (p authedProvider) AuthHint() string                   { return "run the login flow" }

func TestAuthSummary(t *testing.T) {
	provider.Register(authedProvider{name: "csumok"})
	provider.Register(authedProvider{name: "csumanon", err: errors.New("no creds")})

	got := command.AuthSummary(context.Background(), []string{"csumok", "csumanon"})
	require.Equal(t,
		"✓ csumok: authenticated\n• csumanon: anonymous - run the login flow",
		got,
	)

	require.Empty(t, command.AuthSummary(context.Background(), nil), "no providers, no summary")
}
