package gitlab

// apiURL builds the absolute REST API URL for a path on a host, e.g.
// https://gitlab.com/api/v4/projects/.../repository/tags. A self-managed host
// serves the same /api/v4 surface. Requests go through a [forge.RESTClient],
// attaching a token only when there is one, so anonymous (rate-limited) reads
// of a public project still work. Bearer is the one header GitLab accepts for
// both credential kinds: an OAuth token minted by the device flow and a
// personal access token. The PRIVATE-TOKEN header works only for PATs, so a
// stored OAuth token sent that way 401s.
func apiURL(host, path string) string {
	return "https://" + host + "/api/v4/" + path
}
