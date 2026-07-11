package constant

// Provider names, as written in a directive's provider= and used by the
// registry and rewriter dispatch.
const (
	// ProviderAuto asks clover to infer the real provider from context (e.g. a
	// GitHub Actions uses: pin in a workflow file resolves to github).
	ProviderAuto = "auto"
	// ProviderManual is a human-owned root: it publishes the value already on
	// the target line under an id for followers, contacting no upstream.
	ProviderManual = "manual"

	ProviderDocker    = "docker"
	ProviderFollow    = "follow"
	ProviderGitea     = "gitea"
	ProviderGithub    = "github"
	ProviderGitlab    = "gitlab"
	ProviderGo        = "go"
	ProviderHashicorp = "hashicorp"
	ProviderHelm      = "helm"
	ProviderHTTP      = "http"
	ProviderNode      = "node"
	ProviderNpm       = "npm"
	ProviderOpentofu  = "opentofu"
	ProviderPython    = "python"
	ProviderTerraform = "terraform"
	ProviderZig       = "zig"
)
