// Package node resolves Node.js runtime versions from nodejs.org's public,
// unauthenticated release index. The index lists every release newest-first in a
// single response, so discovery is one fetch with no pagination; each candidate
// carries the release date the index supplies. By default it tracks all releases;
// lts restricts to the long-term-support lines. Checksums are left to a follower
// reading the version's predictable SHASUMS256.txt file.
package node
