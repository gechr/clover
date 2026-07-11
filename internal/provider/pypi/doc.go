// Package pypi resolves Python package versions from the official PyPI JSON
// API. One anonymous fetch returns a package's whole release history: a map of
// version to its uploaded files, each carrying an upload time (so cooldown
// applies), a yanked flag, and a sha256 digest.
//
// A PEP 440 prerelease omits the dash (0.5.30rc1); parsing to canonical semver
// restores it (0.5.30-rc1) so the version orders and scheme-matches like any
// other prerelease. Versions outside the semver shape (.dev and .post suffixes,
// epochs) are dropped, as are versions with no files and versions whose every
// file is yanked - neither is installable.
package pypi
