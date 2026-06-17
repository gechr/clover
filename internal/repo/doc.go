// Package repo resolves the git repository a file belongs to, so cusp can
// namespace id= per repository: the same id in two different repositories is not
// a clash. The repository is the nearest ancestor directory holding a .git
// entry; lookups are cached per directory. The namespacing itself (prefixing id
// and from with the root) happens in the pipeline, keeping the executor and
// registry repo-agnostic.
package repo
