package github

import (
	"context"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/forge"
)

// oauthClientID is the public Application (client) ID of the clover OAuth app on
// github.com. The client ID is public - device flow needs no client secret - so
// it is safe to embed. It is github.com-specific: a GitHub Enterprise Server
// instance has its own per-instance app, supplied via --client-id.
const oauthClientID = "Ov23liZM0FWwyeh6cWhp"

// deviceConfig describes GitHub's device flow. github.com serves the endpoints
// on github.com itself (not api.github.com); a GHES host serves the same paths
// under its own origin. The `repo` scope grants read+write to private
// repositories - broader than this read-only tool needs - but it is the
// narrowest classic OAuth scope that can read private tags and releases, and
// device flow cannot issue fine-grained tokens. Public reads need no scope at
// all.
var deviceConfig = forge.DeviceConfig{
	Label:           constant.ProviderGithub,
	DefaultHost:     defaultHost,
	DefaultClientID: oauthClientID,
	ClientIDHint:    "register an OAuth app on the instance",
	Scopes:          []string{"repo"},
	DeviceCodeURL: func(host string) string {
		return "https://" + host + "/login/device/code"
	},
	AccessTokenURL: func(host string) string {
		return "https://" + host + "/login/oauth/access_token"
	},
}

// Login runs the GitHub device flow against host and stores the minted token
// under it; see [forge.DeviceLogin].
func Login(ctx context.Context, host, clientID string, prompt func(forge.Code)) error {
	return forge.DeviceLogin(ctx, deviceConfig, host, clientID, prompt)
}
