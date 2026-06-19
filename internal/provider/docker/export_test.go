package docker

// ParseChallenge exposes parseChallenge for black-box testing of the bearer
// WWW-Authenticate parsing in isolation.
var ParseChallenge = parseChallenge

// NextLink exposes nextLink for black-box testing of Link-header pagination and
// its same-host guard.
var NextLink = nextLink

// AuthHint exposes the auth-hint text so error assertions can match it exactly.
const AuthHint = authHint
