package gitlab

import (
	"context"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/forge"
)

// oauthClientID is the public Application ID of the "clover" OAuth application
// registered on gitlab.com (User Settings > Applications, Device authorization
// grant enabled, read_api scope). Device flow is a public client - no client
// secret - so the ID is safe to embed. It is gitlab.com-specific: a self-managed
// instance has its own application and ID, supplied via --client-id.
const oauthClientID = "44645c75a26f9ad0817f292aef95996b65618d2343e4e0a04a549df4ae75f4f4"

// deviceConfig describes GitLab's device flow. A self-managed instance serves
// the same endpoint paths under its own origin. The read_api scope is read-only
// API access, enough to read a private project's tags and releases and nothing
// more.
var deviceConfig = forge.DeviceConfig{
	Label:           constant.ProviderGitlab,
	DefaultHost:     defaultHost,
	DefaultClientID: oauthClientID,
	ClientIDHint:    "register an application on the instance",
	Scopes:          []string{"read_api"},
	DeviceCodeURL: func(host string) string {
		return "https://" + host + "/oauth/authorize_device"
	},
	AccessTokenURL: func(host string) string {
		return "https://" + host + "/oauth/token"
	},
}

// Login runs the GitLab device flow against host and stores the minted token
// under it; see [forge.DeviceLogin].
func Login(ctx context.Context, host, clientID string, prompt func(forge.Code)) error {
	return forge.DeviceLogin(ctx, deviceConfig, host, clientID, prompt)
}
