// Package npm resolves npm package versions from an npm registry. A package's
// packument lists every published version and its publication date in a single
// response, so discovery is one fetch with no pagination; each candidate
// carries the date the registry supplies and its tarball as an asset, which a
// sha256 follower can hash. The package key names the package to track, scoped
// names (@scope/name) included; the dist-tag key narrows the candidates to the
// version a registry channel pointer names; the deprecated key keeps deprecated
// versions eligible; the registry key swaps the public registry for any
// npm-compatible base URL.
package npm
