// Package all is the composition helper that constructs every built-in provider.
// It is the single source of the built-in provider set: the binary's composition
// root and the tests that need the full registry both call [New], so the list
// cannot drift between them - in particular the schema-coverage guard checks every
// provider's keys against the same set the binary ships.
package all

import (
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/docker"
	"github.com/gechr/clover/internal/provider/gitea"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/gechr/clover/internal/provider/hashicorp"
	"github.com/gechr/clover/internal/provider/helm"
	"github.com/gechr/clover/internal/provider/http"
	"github.com/gechr/clover/internal/provider/manual"
	"github.com/gechr/clover/internal/provider/node"
)

// New constructs every built-in provider, in registration order. httpOpts
// configure the http provider alone - the binary passes its version, tests pass
// none - so a caller that needs no http configuration gets the same http provider
// the zero-option constructor builds.
func New(httpOpts ...http.Option) []provider.Provider {
	return []provider.Provider{
		docker.New(),
		gitea.New(),
		github.New(),
		gitlab.New(),
		hashicorp.New(),
		helm.New(),
		http.New(httpOpts...),
		manual.New(),
		node.New(),
	}
}
