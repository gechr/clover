package constant

// Provider names, as written in a directive's provider= and used by the
// registry and rewriter dispatch.
const (
	// ProviderAuto asks clover to infer the real provider from context (e.g. a
	// GitHub Actions uses: pin in a workflow file resolves to github).
	ProviderAuto   = "auto"
	ProviderFollow = "follow"
	ProviderGithub = "github"
)
