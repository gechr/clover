// Package vcs resolves the version-controlled repository a file belongs to, so
// clover can namespace id= per repository: the same id in two different
// repositories is not a clash. The repository is the nearest ancestor directory
// holding a VCS marker (.git, .jj, .hg, .svn); lookups are cached per directory.
// The namespacing itself (prefixing id and from with the root) happens in the
// pipeline, keeping the executor and registry repo-agnostic.
package vcs
