package forge

import (
	"cmp"
	"context"
	"fmt"
	"net/http"

	"github.com/cli/oauth/device"
	"github.com/gechr/clover/internal/token"
)

// DeviceConfig describes one forge's OAuth device flow: the label framing its
// errors, the default host and the public client ID embedded for it, the hint
// shown when a private instance needs --client-id, the scopes to request, and
// the host-relative endpoints.
type DeviceConfig struct {
	Label           string
	DefaultHost     string
	DefaultClientID string
	ClientIDHint    string
	Scopes          []string
	DeviceCodeURL   func(host string) string
	AccessTokenURL  func(host string) string
}

// Code is the user-facing half of the device flow: the one-time code to enter
// and the URL to enter it at.
type Code struct {
	UserCode        string
	VerificationURL string
}

// DeviceLogin runs the OAuth device flow cfg describes against host and stores
// the minted token: it requests a code, hands it to prompt so the caller can
// show it to the user, polls until the user authorises in the browser, then
// persists the token under the host so the credential chain finds it. It needs
// no client secret and binds no local port, so it works headless. host and
// clientID default to cfg's host and embedded app; a private instance has no
// embeddable app, so it requires an explicit --client-id. The context bounds
// the poll.
func DeviceLogin(
	ctx context.Context,
	cfg DeviceConfig,
	host, clientID string,
	prompt func(Code),
) error {
	host, ok := NormalizeHost(cmp.Or(host, cfg.DefaultHost))
	if !ok {
		return fmt.Errorf("%s: invalid host", cfg.Label)
	}
	if host == cfg.DefaultHost {
		clientID = cmp.Or(clientID, cfg.DefaultClientID)
	}
	if clientID == "" {
		return fmt.Errorf("%s: %s requires --client-id (%s)", cfg.Label, host, cfg.ClientIDHint)
	}

	httpClient := &http.Client{}

	code, err := device.RequestCode(httpClient, cfg.DeviceCodeURL(host), clientID, cfg.Scopes)
	if err != nil {
		return fmt.Errorf("%s: request device code: %w", cfg.Label, err)
	}

	prompt(Code{UserCode: code.UserCode, VerificationURL: code.VerificationURI})

	accessToken, err := device.Wait(ctx, httpClient, cfg.AccessTokenURL(host), device.WaitOptions{
		ClientID:   clientID,
		DeviceCode: code,
	})
	if err != nil {
		return fmt.Errorf("%s: authorize device: %w", cfg.Label, err)
	}
	if accessToken.Token == "" {
		return fmt.Errorf("%s: device flow returned an empty token", cfg.Label)
	}

	store, err := token.New()
	if err != nil {
		return err
	}
	if err := store.Set(host, accessToken.Token); err != nil {
		return fmt.Errorf("%s: store token: %w", cfg.Label, err)
	}
	return nil
}
