package provider

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// StatusError formats a failed HTTP response as "<action>: <body> (<status>)",
// reading the body for the upstream's own message.
func StatusError(action string, resp *http.Response) error {
	msg, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("%s: %s (%s)", action, strings.TrimSpace(string(msg)), resp.Status)
}
