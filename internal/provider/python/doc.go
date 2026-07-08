// Package python resolves CPython releases from the official python.org
// downloads API. The API is a single, anonymous JSON array of every release,
// each carrying a publication date (so cooldown applies) and a prerelease flag,
// with the version embedded in a "Python X.Y.Z" name.
//
// The version's "Python " prefix is stripped and the result parsed to canonical
// semver, which restores the dash a python.org prerelease omits (3.15.0b3 ->
// 3.15.0-b3) so it orders and scheme-matches like any other prerelease.
package python
