package constant

// Follow value kinds: what a follow marker projects from the producer it
// follows (value=). Version and Commit are read straight from the producer's
// candidate; Sha256 is fetched using the producer's version.
const (
	ValueCommit  = "commit"
	ValueSha256  = "sha256"
	ValueVersion = "version"
)
