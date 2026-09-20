package core

import (
	"context"
	"time"
)

const (
	authURLPollInterval = 200 * time.Millisecond
	authURLTimeout      = 10 * time.Second
	runningPollInterval = 500 * time.Millisecond
)

// StartLoginInteractive asks the control plane for a login URL. The URL itself
// is returned by LoginURL, reported through Watch (EventAuthURL) and echoed on
// Config.UserLogf.
//
// The host app should open the URL in the system browser (Custom Tabs on
// Android, ASWebAuthenticationSession/SFSafariViewController on Apple
// platforms). No redirect URI is needed: the node learns about the completed
// login through its control connection.
func (n *Node) StartLoginInteractive(ctx context.Context) error {
	lc, err := n.LocalClient()
	if err != nil {
		return err
	}
	if err := lc.StartLoginInteractive(ctx); err != nil {
		return newError(ErrCodeBackend, "login", "unable to start interactive login", err)
	}
	return nil
}

// LoginURL returns the URL the user has to open to authorize this node.
//
// If the backend has not produced a URL yet, LoginURL triggers interactive
// login and waits (up to 10s) for the control plane to hand one over. It
// returns a timeout error when the node is already authorized or is waiting
// for admin approval instead.
func (n *Node) LoginURL(ctx context.Context) (string, error) {
	st, err := n.StatusWithoutPeers(ctx)
	if err != nil {
		return "", err
	}
	if st.AuthURL != "" {
		return st.AuthURL, nil
	}
	if err := n.StartLoginInteractive(ctx); err != nil {
		return "", err
	}
	deadline := time.Now().Add(authURLTimeout)
	for {
		select {
		case <-ctx.Done():
			return "", newError(ErrCodeTimeout, "login-url", "canceled while waiting for the auth URL", ctx.Err())
		case <-time.After(authURLPollInterval):
		}
		st, err := n.StatusWithoutPeers(ctx)
		if err != nil {
			return "", err
		}
		switch {
		case st.AuthURL != "":
			return st.AuthURL, nil
		case st.State.Running():
			return "", newError(ErrCodeBackend, "login-url", "node is already authorized", nil)
		case time.Now().After(deadline):
			return "", newError(ErrCodeTimeout, "login-url",
				"no auth URL was provided; the node may already be authorized or waiting for machine approval", nil)
		}
	}
}

// WaitForRunning blocks until the node is authorized and usable, and returns
// the resulting status (including the device list).
//
// It returns ErrCodeLoginRequired as soon as the tailnet requires an admin to
// approve the machine, so the UI can tell the user what to do instead of
// spinning.
func (n *Node) WaitForRunning(ctx context.Context) (*Status, error) {
	for {
		st, err := n.StatusWithoutPeers(ctx)
		if err != nil {
			return nil, err
		}
		switch st.State {
		case StateRunning:
			return n.Status(ctx)
		case StateNeedsMachineAuth:
			return nil, newError(ErrCodeLoginRequired, "wait",
				"node is waiting to be approved in the tailnet admin console", nil)
		}
		select {
		case <-ctx.Done():
			return nil, newError(ErrCodeTimeout, "wait",
				"timed out waiting for the node to come up (last state: "+string(st.State)+")", ctx.Err())
		case <-time.After(runningPollInterval):
		}
	}
}
