package github

// apiURL builds the absolute REST API URL for a path on a host, mirroring go-gh's
// host mapping: github.com is served at api.github.com, a GitHub Enterprise
// Server host under https://<host>/api/v3. Requests go through a
// [forge.RESTClient]; go-gh's own REST client refuses to build without a
// resolvable token, which would break anonymous access (and force tests to find
// an ambient credential), so clover issues REST requests directly instead.
func apiURL(host, path string) string {
	if host == defaultHost {
		return "https://api.github.com/" + path
	}
	return "https://" + host + "/api/v3/" + path
}
