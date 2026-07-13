// Package swift resolves Swift toolchain versions from the official swift.org
// release index. The index is a single, anonymous JSON listing of every release,
// each entry carrying its tag, publication date, and per-platform SDK checksums
// for free, so the provider fetches it once and lets the framework own selection.
//
// The entry name is the bare version (5.10, 6.3.3), matching a bare on-line
// reference, while the tag (swift-6.3.3-RELEASE) is the upstream ref the Linker
// resolves to a web page.
package swift
