package forge

import (
	"errors"
	"net/http"
)

// maxRedirects mirrors net/http's default redirect ceiling, which a custom
// CheckRedirect replaces rather than inherits.
const maxRedirects = 10

// DropAuthRedirect is the CheckRedirect policy for credentialed downloads: it
// removes the Authorization header whenever a redirect leaves the original
// request's exact origin (scheme and host). Go's default policy keeps the
// header on a subdomain hop and on an https->http downgrade of the same host,
// either of which would hand the credential to a host the user did not name.
func DropAuthRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("stopped after 10 redirects")
	}
	first := via[0].URL
	if req.URL.Scheme != first.Scheme || req.URL.Host != first.Host {
		req.Header.Del("Authorization")
	}
	return nil
}
