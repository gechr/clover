package forge

import (
	"cmp"
	"os"
)

// PATHost returns the single host a host-independent personal access token may be
// sent to. A PAT is attached to whichever host a marker names, so a
// marker-controlled host= could otherwise redirect the token to an attacker; the
// PAT is bound to one host - the hostEnv override (normalized) when set, else
// defaultHost. When pinned (a test transport), ambient env is ignored and
// defaultHost is returned, keeping a test hermetic and its auth path deterministic.
func PATHost(hostEnv, defaultHost string, pinned bool) string {
	if pinned {
		return defaultHost
	}
	if h, ok := NormalizeHost(cmp.Or(os.Getenv(hostEnv), defaultHost)); ok {
		return h
	}
	return defaultHost
}
