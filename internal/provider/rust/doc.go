// Package rust resolves Rust toolchain versions from the official
// static.rust-lang.org manifest index. The index is a single, anonymous,
// chronological text listing of every channel manifest ever published; the
// version-named manifests in it (channel-rust-1.97.0.toml) enumerate the
// releases, each dated by the directory it was published under, so the provider
// fetches the index once and lets the framework own selection.
//
// The channel key picks the release channel to track: stable (the default) or
// beta, whose dated snapshots are numbered (1.98.0-beta.1). Nightly builds are
// dated snapshots without a version of their own, so they cannot be tracked.
package rust
