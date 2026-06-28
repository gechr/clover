// Package forge holds provider-agnostic helpers shared by forge providers -
// Gitea/Forgejo today, GitHub Enterprise and self-managed GitLab in future: host
// parsing, same-origin checks, and OAuth PKCE/Link primitives. Everything here is
// pure and stateless; each provider keeps its own endpoints, token store, and
// auth flow.
package forge

import (
	"net/url"
	"strings"
)

// NormalizeHost parses a forge host given as a bare name (codeberg.org), a
// host:port, or a full URL (https://git.example.com/), returning the lowercased
// host[:port]. ok is false when the value is empty or carries userinfo, a path,
// query, or fragment - anything that is not a plain network authority - so a
// marker-controlled host cannot smuggle a path or credentials.
func NormalizeHost(host string) (string, bool) {
	raw := host
	if !strings.Contains(raw, "://") {
		raw = "//" + raw // make url.Parse read the value as an authority
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil, u.Host == "", u.User != nil,
		u.RawQuery != "", u.Fragment != "", strings.Trim(u.Path, "/") != "":
		return "", false
	}
	return strings.ToLower(u.Host), true
}

// SameOrigin reports whether two URLs share a scheme and host. It guards a
// paginated lookup from forwarding a credential to a different origin than the
// one the lookup started on.
func SameOrigin(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	return ua.Scheme == ub.Scheme && ua.Host == ub.Host
}
