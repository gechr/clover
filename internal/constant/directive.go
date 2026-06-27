package constant

// DirectiveEqual separates a directive key from its value. It is a rune because
// the parser scans character by character; use string(DirectiveEqual) where a
// string is needed (e.g. rendering a directive in format mode).
const DirectiveEqual = '='

// DirectiveKeyword is the sigil clover scans for inside a comment. Everything
// after it on the line is the directive the user wrote.
const DirectiveKeyword = "clover:"

// Directive targeting and control keys: who resolves the marker and how it
// relates to others.
const (
	DirectiveFind     = "find"     // explicit find pattern (glob with <placeholders>, or /regex/)
	DirectiveFrom     = "from"     // follow the producer with this id
	DirectiveID       = "id"       // publish this marker's result under this id
	DirectiveReplace  = "replace"  // explicit replace template, pairs with find
	DirectiveProvider = "provider" // upstream source; omitted ⇒ follow
	DirectiveSelect   = "select"   // follow the old or new value
	DirectiveSkip     = "skip"     // disable this marker
	DirectiveTags     = "tags"     // comma-separated labels for --tags filtering
	DirectiveTrack    = "track"    // track a floating ref (docker tag, github branch); * infers it from the line
	DirectiveValue    = "value"    // what a follower projects
	DirectiveVerify   = "verify"   // deep-verify this marker's secure pin against upstream

	DirectiveVerifyBranch = "verify-branch" // allowed source-branch glob or /regex/ for deep verify (default: the repo's default branch)

	DirectivePattern      = "pattern"       // asset filename glob for value=sha256
	DirectiveSha256Source = "sha256-source" // how to source a value=sha256 (see constant/value.go)
	DirectiveSha256URL    = "sha256-url"    // checksum-file URL (templated with {version}) for value=sha256
)

// TrackInfer is the track= value that infers the floating ref from the target
// line (the literal tag or branch already written there) rather than naming it.
const TrackInfer = "*"

// Provider parameters shared beyond a single provider: the auto-inference
// injects them and the relevant providers read them.
const (
	DirectiveBuild      = "build"      // hashicorp build-metadata flavor, e.g. ent.hsm.fips1403 (hashicorp)
	DirectiveChart      = "chart"      // helm chart name (helm)
	DirectiveEnterprise = "enterprise" // track enterprise-licensed builds (hashicorp)
	DirectiveLTS        = "lts"        // restrict to LTS release lines (node)
	DirectivePlatform   = "platform"   // OCI platform os/arch for per-arch digest resolution (docker)
	DirectiveProduct    = "product"    // hashicorp product name, e.g. terraform (hashicorp)
	DirectiveRegistry   = "registry"   // container registry host (docker); chart repository URL or oci:// base (helm)
	DirectiveRepository = "repository" // repository path (github, docker)
)
