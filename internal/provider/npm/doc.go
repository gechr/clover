// Package npm resolves npm package versions from the public npm registry. A
// package's packument lists every published version and its publication date in
// a single response, so discovery is one fetch with no pagination; each
// candidate carries the date the registry supplies and its tarball as an asset,
// which a sha256 follower can hash. The package key names the package to track,
// scoped names (@scope/name) included.
package npm
