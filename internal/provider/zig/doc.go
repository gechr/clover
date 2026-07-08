// Package zig resolves Zig toolchain versions from the official ziglang.org
// download index. The index is a single, anonymous JSON object keyed by version,
// each entry carrying its per-platform archive checksums and publication date for
// free, so the provider fetches it once and lets the framework own selection.
//
// The index key is the authoritative version: recent entries also carry a
// redundant "version" field, but older releases omit it, so the provider always
// reads the key. The "master" key is a moving nightly pointer, not a release, and
// is skipped.
package zig
