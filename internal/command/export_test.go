package command

import (
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/output"
)

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

// ApplyTo drives (*cmdRun).applyTo from a black-box test: cmdRun is unexported,
// so tests cannot build it directly. It returns the cooldown, downgrade, and
// force fields after the --to implications are applied.
func ApplyTo(to, cooldown string, downgrade, force *bool) (string, *bool, *bool) {
	c := &cmdRun{To: to, Cooldown: cooldown, Downgrade: downgrade, Force: force}
	c.applyTo()
	return c.Cooldown, c.Downgrade, c.Force
}

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

// Additional re-exports for black-box tests of the command Run methods and their
// report helpers, which reach unexported functions and command structs.
var (
	AnnotateDiscovered = annotateDiscovered
	CompleteProviders  = completeProviders
	CompleteTags       = completeTags
	CompletionHandler  = completionHandler
	EnableHTTPCache    = enableHTTPCache
	Launch             = launch
	PromptBrowser      = promptBrowser
	PromptDeviceCode   = promptDeviceCode
	ReportAuth         = reportAuth
	ReportDeep         = reportDeep
	ReportExit         = reportExit
	UpdateConfig       = updateConfig
)

// Predictor kind constants the completion handler dispatches on.
const (
	PredictorProvider = predictorProvider
	PredictorTag      = predictorTag
)

// RunVersion drives (*cmdVersion).Run from a black-box test.
func RunVersion(detailed bool) error { return (&cmdVersion{Detailed: detailed}).Run() }

// RunLogin drives (*cmdLogin).Run from a black-box test.
func RunLogin(provider string) error { return (&cmdLogin{Provider: provider}).Run() }

// RunInit drives (*cmdInit).Run from a black-box test.
func RunInit(dir string) error { return (&cmdInit{Dir: dir}).Run() }

// RunAnnotate drives (*cmdAnnotate).Run from a black-box test.
func RunAnnotate(
	paths []string,
	dryRun, write, check *bool,
	force, noIgnore bool,
	cfg *config.Resolver,
	workers int,
) error {
	c := &cmdAnnotate{
		Paths:    paths,
		DryRun:   dryRun,
		Write:    write,
		Check:    check,
		Force:    force,
		NoIgnore: noIgnore,
	}
	return c.Run(cfg, parallelism(workers))
}

// RunFormat drives (*cmdFormat).Run from a black-box test.
func RunFormat(
	paths []string,
	check, dryRun bool,
	prune *bool,
	noIgnore bool,
	cfg *config.Resolver,
	workers int,
) error {
	c := &cmdFormat{Paths: paths, Check: check, DryRun: dryRun, Prune: prune, NoIgnore: noIgnore}
	return c.Run(cfg, parallelism(workers))
}

// RunLint drives (*cmdLint).Run from a black-box test.
func RunLint(
	paths, tags []string,
	out *output.Mode,
	noIgnore bool,
	cfg *config.Resolver,
	workers int,
) error {
	c := &cmdLint{Paths: paths, Tags: tags, Output: out, NoIgnore: noIgnore}
	return c.Run(cfg, parallelism(workers))
}

// RunRun drives (*cmdRun).Run from a black-box test.
func RunRun(
	paths []string,
	dryRun, infer bool,
	enable, disable, tags []string,
	cooldown string,
	out *output.Mode,
	cfg *config.Resolver,
	workers int,
) error {
	c := &cmdRun{
		Paths:    paths,
		DryRun:   dryRun,
		Infer:    infer,
		Enable:   enable,
		Disable:  disable,
		Tags:     tags,
		Cooldown: cooldown,
		Output:   out,
	}
	return c.Run(cfg, parallelism(workers))
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
