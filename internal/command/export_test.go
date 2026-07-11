package command

import "github.com/gechr/clover/internal/config"

// Exposed for black-box tests of the package's pure helpers.
var (
	AuthSummary        = authSummary
	CacheEnabled       = cacheEnabled
	ConfirmDeep        = confirmDeep
	CooldownOverride   = cooldownOverride
	DeepHints          = deepHints
	DefaultConstraint  = defaultConstraint
	NoCandidateDeep    = noCandidateDeep
	ProviderFilter     = providerFilter
	Roots              = roots
	RunErr             = runErr
	TagFilter          = tagFilter
	UsedProviders      = usedProviders
	ValidateConstraint = validateConstraint
	WriteFailures      = writeFailures
)

// FailuresError exposes the unexported exit-status error type so tests can
// assert its formatting directly.
type FailuresError = failuresError

// AnnotateMode drives (*cmdAnnotate).mode from a black-box test: cmdAnnotate is
// unexported, so tests cannot build it directly.
func AnnotateMode(check, dryRun, write *bool, cfg *config.Config) (bool, bool) {
	c := &cmdAnnotate{Check: check, DryRun: dryRun, Write: write}
	return c.mode(cfg)
}

// NewResolver drives newResolver from a black-box test: the root command tree
// is unexported, so tests cannot build it directly.
func NewResolver(cfgPath string, noConfig bool) (*config.Resolver, error) {
	return newResolver(root{Config: cfgPath, NoConfig: noConfig})
}

// Helps returns each command's detailed --help blurb, keyed by command name, so
// a black-box test can assert them without reaching the unexported cmd types.
func Helps() map[string]string {
	return map[string]string{
		"annotate": (&cmdAnnotate{}).Help(),
		"format":   (&cmdFormat{}).Help(),
		"init":     (&cmdInit{}).Help(),
		"lint":     (&cmdLint{}).Help(),
		"login":    (&cmdLogin{}).Help(),
		"run":      (&cmdRun{}).Help(),
		"update":   (&cmdUpdate{}).Help(),
	}
}
