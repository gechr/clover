package gitea

// apiURL builds the absolute API URL for a path on a host, e.g.
// https://codeberg.org/api/v1/repos/owner/name/tags. Requests go through a
// [forge.RESTClient]; Gitea reads a personal access token as `token <tok>` and
// an OAuth access token as `Bearer <tok>`, so auth supplies the scheme per
// credential.
func apiURL(host, path string) string {
	return "https://" + host + "/api/v1/" + path
}
