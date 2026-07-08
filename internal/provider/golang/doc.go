// Package golang resolves Go toolchain versions from the official go.dev
// download index. The index is a single, anonymous, newest-first JSON listing of
// every release, each entry carrying its per-platform archive checksums for free,
// so the provider fetches it once and lets the framework own selection.
//
// The package is named golang because go is a reserved word; the provider
// identifies itself as "go" (see [constant.ProviderGo]). go.dev version strings
// carry a "go" prefix and a dashless prerelease form (go1.26.5, go1.27rc1); the
// prefix is stripped so the resolved value is clean semver, while the prefixed
// form is retained on the candidate's Ref for links.
package golang
