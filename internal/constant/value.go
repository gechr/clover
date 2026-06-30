package constant

// Follow value kinds: what a follow marker projects from the producer it
// follows (value=). Version and Commit are read straight from the producer's
// candidate; Sha256 is fetched using the producer's version.
const (
	ValueCommit  = "commit"
	ValueSha256  = "sha256"
	ValueVersion = "version"
)

// Sha256 sources: how a value=sha256 follower obtains its checksum (sha256-source=).
// Auto tries them in order; verify cross-checks the provider digest against a
// published checksums file and fails on a mismatch.
const (
	Sha256Auto      = "auto"      // digest, then checksums, then download (default)
	Sha256Digest    = "digest"    // the provider's asset digest, no download
	Sha256Checksums = "checksums" // a published checksums file (sha256-url or a sibling asset)
	Sha256Download  = "download"  // download the asset and hash it
	Sha256Verify    = "verify"    // require the digest and checksums file to agree
)

// DigestSha256 is the algorithm prefix for a sha256 content digest ("sha256:<hex>").
const DigestSha256 = "sha256:"

// DockerDigestMarker is the separator plus digest prefix for a digest-pinned
// container image reference ("@sha256:<hex>").
const DockerDigestMarker = "@" + DigestSha256
