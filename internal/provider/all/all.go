// Package all is the composition helper that constructs every built-in provider.
// It is the single source of the built-in provider set: the binary's composition
// root and the tests that need the full registry both call [New], so the list
// cannot drift between them - in particular the schema-coverage guard checks every
// provider's keys against the same set the binary ships.
package all

import (
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/crates"
	"github.com/gechr/clover/internal/provider/docker"
	"github.com/gechr/clover/internal/provider/gitea"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/gechr/clover/internal/provider/golang"
	"github.com/gechr/clover/internal/provider/hashicorp"
	"github.com/gechr/clover/internal/provider/helm"
	"github.com/gechr/clover/internal/provider/http"
	"github.com/gechr/clover/internal/provider/manual"
	"github.com/gechr/clover/internal/provider/node"
	"github.com/gechr/clover/internal/provider/npm"
	"github.com/gechr/clover/internal/provider/pypi"
	"github.com/gechr/clover/internal/provider/python"
	"github.com/gechr/clover/internal/provider/rust"
	"github.com/gechr/clover/internal/provider/terraform"
	"github.com/gechr/clover/internal/provider/zig"
)

// New constructs every built-in provider, in registration order. version is
// the running binary's version, woven into the User-Agent of the providers
// that identify themselves - the binary passes clive.Current(), tests pass an
// empty string for the versionless fallback.
func New(version string) []provider.Provider {
	return []provider.Provider{
		crates.New(crates.WithVersion(version)),
		docker.New(),
		gitea.New(),
		github.New(),
		gitlab.New(),
		golang.New(),
		hashicorp.New(),
		helm.New(),
		http.New(http.WithVersion(version)),
		manual.New(),
		node.New(),
		npm.New(),
		pypi.New(),
		python.New(),
		rust.New(),
		terraform.New(terraform.Terraform),
		terraform.New(terraform.OpenTofu),
		zig.New(),
	}
}
