package constant

// VersionDash separates a version's numeric core from its prerelease or variant
// suffix (1.27-alpine, 2.0.0-rc.1). It is a rune because the recognizer scans
// byte by byte; use string(VersionDash) where a string is needed.
const VersionDash = '-'
