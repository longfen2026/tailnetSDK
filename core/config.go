package core

import (
	"os"
	"path/filepath"
	"strings"

	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
)

const (
	// DefaultControlURL is the official Tailscale coordination server.
	DefaultControlURL = "https://controlplane.tailscale.com"

	// DefaultHostname is used when Config.Hostname is empty.
	DefaultHostname = "tailnetsdk"
)

// Config describes one embedded tailnet node.
//
// Only Dir is mandatory on platforms without a user config directory (all
// mobile platforms); everywhere else Dir defaults to
// <UserConfigDir>/tailnetsdk/<Hostname>. The state directory contains the node
// key, so it must be private (0700) and, on mobile, backed by the platform
// keystore in later milestones.
type Config struct {
	// Dir is the state directory. Empty means "derive a default".
	Dir string

	// Hostname is the device name that shows up in the tailnet admin console.
	Hostname string

	// ControlURL overrides the coordination server. Empty means the official
	// Tailscale control plane.
	ControlURL string

	// Ephemeral registers the node as an ephemeral node: it disappears from the
	// tailnet when it disconnects.
	Ephemeral bool

	// AuthKey, if set, authorizes the node without user interaction. Leave it
	// empty for interactive (browser) login.
	AuthKey string

	// ClientSecret enables OAuth-based key minting. Only supported when the
	// binary imports tailscale.com/feature/oauthclient (not linked by default).
	ClientSecret string

	// AdvertiseTags are ACL tags requested for this node.
	AdvertiseTags []string

	// EnableProxy starts the built-in loopback SOCKS5/HTTP proxy during Start,
	// so that non-Go code in the host app can send traffic over the tailnet.
	EnableProxy bool

	// Logf receives verbose debugging logs. Nil discards them.
	Logf func(format string, args ...any)

	// UserLogf receives user-facing logs such as the login URL. Nil means the
	// SDK only surfaces the URL through LoginURL/Watch.
	UserLogf func(format string, args ...any)
}

// withDefaults validates the configuration and fills in defaults.
func (c Config) withDefaults() (Config, error) {
	out := c
	if strings.TrimSpace(out.Hostname) == "" {
		out.Hostname = DefaultHostname
	}
	if strings.TrimSpace(out.ControlURL) == "" {
		out.ControlURL = DefaultControlURL
	}
	if strings.TrimSpace(out.Dir) == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return out, newError(ErrCodeInvalidArgument, "config",
				"Dir must be set: this platform has no user config directory", err)
		}
		out.Dir = filepath.Join(base, "tailnetsdk", out.Hostname)
	}
	abs, err := filepath.Abs(out.Dir)
	if err != nil {
		return out, newError(ErrCodeInvalidArgument, "config", "invalid Dir "+out.Dir, err)
	}
	out.Dir = abs
	return out, nil
}

// server builds the underlying tsnet.Server. It is called by Node.Start.
func (c Config) server() (*tsnet.Server, error) {
	resolved, err := c.withDefaults()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(resolved.Dir, 0o700); err != nil {
		return nil, newError(ErrCodeInvalidArgument, "config",
			"unable to create state directory "+resolved.Dir, err)
	}
	s := &tsnet.Server{
		Dir:           resolved.Dir,
		Hostname:      resolved.Hostname,
		ControlURL:    resolved.ControlURL,
		Ephemeral:     resolved.Ephemeral,
		AuthKey:       resolved.AuthKey,
		ClientSecret:  resolved.ClientSecret,
		AdvertiseTags: resolved.AdvertiseTags,
		Logf:          logger.Discard,
	}
	if resolved.Logf != nil {
		s.Logf = resolved.Logf
	}
	if resolved.UserLogf != nil {
		s.UserLogf = resolved.UserLogf
	}
	return s, nil
}
