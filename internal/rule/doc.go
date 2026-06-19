// Package rule compiles a directive's selection-policy keys (constraint,
// include/exclude, prerelease, cooldown, behind, downgrade) into the
// options the version selection chain consumes. It is the bridge between the
// directive grammar and version.Select: the directive layer enforces value
// types, this layer enforces each key's meaning (e.g. behind must be
// non-negative, include/exclude compile to pattern predicates).
package rule
