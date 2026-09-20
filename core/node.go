package core

import (
	"context"
	"net/http"
	"net/netip"
	"sync"

	"tailscale.com/client/local"
	"tailscale.com/tsnet"
)

// Node is a single embedded tailnet node: one Tailscale device that lives
// inside the host process. It owns the tsnet.Server plus the in-process
// LocalAPI client used for authentication, status and session operations.
//
// A Node is safe for concurrent use; Start and Close are serialized against
// each other.
type Node struct {
	cfg Config

	startMu sync.Mutex // serializes Start and Close

	mu        sync.RWMutex
	srv       *tsnet.Server
	lc        *local.Client
	started   bool
	closed    bool
	proxyAddr string
	proxyCred string
}

// New prepares a node. No network activity happens before Start or Up.
func New(cfg Config) (*Node, error) {
	resolved, err := cfg.withDefaults()
	if err != nil {
		return nil, err
	}
	return &Node{cfg: resolved}, nil
}

// Config returns the effective configuration with defaults applied.
func (n *Node) Config() Config { return n.cfg }

// Start brings the node up: it joins the tailnet, creating a device on first
// run. If the node still needs authorization, Start returns successfully and
// the auth URL is available via LoginURL, Status or Watch.
//
// Start blocks until the backend has started; canceling ctx aborts the start.
func (n *Node) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	n.startMu.Lock()
	defer n.startMu.Unlock()

	n.mu.RLock()
	started, closed := n.started, n.closed
	n.mu.RUnlock()
	if closed {
		return newError(ErrCodeClosed, "start", "node is closed", nil)
	}
	if started {
		return nil
	}

	srv, err := n.cfg.server()
	if err != nil {
		return err
	}

	// tsnet.Server.Start has no context parameter; cancellation is done by
	// closing the server, so run it in a goroutine.
	errc := make(chan error, 1)
	go func() { errc <- srv.Start() }()
	select {
	case err := <-errc:
		if err != nil {
			_ = srv.Close()
			return newError(ErrCodeBackend, "start", "tsnet failed to start", err)
		}
	case <-ctx.Done():
		_ = srv.Close()
		return newError(ErrCodeTimeout, "start", "start canceled", ctx.Err())
	}

	lc, err := srv.LocalClient()
	if err != nil {
		_ = srv.Close()
		return newError(ErrCodeBackend, "start", "unable to obtain the LocalAPI client", err)
	}

	n.mu.Lock()
	n.srv = srv
	n.lc = lc
	n.started = true
	n.mu.Unlock()

	if n.cfg.EnableProxy {
		if addr, cred, _, perr := srv.Loopback(); perr != nil {
			n.logf("core: loopback proxy unavailable: %v", perr)
		} else {
			n.mu.Lock()
			n.proxyAddr, n.proxyCred = addr, cred
			n.mu.Unlock()
		}
	}
	return nil
}

// Up starts the node and waits until it is connected and usable.
// The returned Status reflects the state right after the node came up.
func (n *Node) Up(ctx context.Context) (*Status, error) {
	if err := n.Start(ctx); err != nil {
		return nil, err
	}
	srv, err := n.Server()
	if err != nil {
		return nil, err
	}
	st, err := srv.Up(ctx)
	if err != nil {
		return nil, newError(ErrCodeBackend, "up", "node did not become usable", err)
	}
	return mapStatus(st), nil
}

// Close shuts the node down and releases all resources. It is idempotent.
func (n *Node) Close() error {
	n.startMu.Lock()
	defer n.startMu.Unlock()

	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return nil
	}
	n.closed = true
	srv := n.srv
	n.srv, n.lc, n.started = nil, nil, false
	n.proxyAddr, n.proxyCred = "", ""
	n.mu.Unlock()

	if srv == nil {
		return nil
	}
	if err := srv.Close(); err != nil {
		return newError(ErrCodeBackend, "close", "tsnet failed to close cleanly", err)
	}
	return nil
}

// Started reports whether the node has been started successfully.
func (n *Node) Started() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.started
}

// Server returns the underlying tsnet.Server as an escape hatch for advanced
// users. Prefer the higher level SDK methods.
func (n *Node) Server() (*tsnet.Server, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.srv == nil {
		return nil, newError(ErrCodeNotStarted, "server", "node is not started", nil)
	}
	return n.srv, nil
}

// LocalClient returns the in-process LocalAPI client. It never traverses a
// loopback TCP listener, so it keeps working after the host process has been
// suspended and resumed (a documented failure mode on iOS).
func (n *Node) LocalClient() (*local.Client, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.lc == nil {
		return nil, newError(ErrCodeNotStarted, "local-client", "node is not started", nil)
	}
	return n.lc, nil
}

// LocalIPs returns the node's own tailnet IPv4 and IPv6 addresses. Both are
// invalid (netip.Addr.IsValid() == false) until the node is authorized.
func (n *Node) LocalIPs() (netip.Addr, netip.Addr) {
	srv, err := n.Server()
	if err != nil {
		return netip.Addr{}, netip.Addr{}
	}
	return srv.TailscaleIPs()
}

// RootPath returns the directory holding this node's state.
func (n *Node) RootPath() string {
	srv, err := n.Server()
	if err != nil {
		return n.cfg.Dir
	}
	return srv.GetRootPath()
}

// HTTPClient returns an HTTP client whose requests are routed over the tailnet.
func (n *Node) HTTPClient() (*http.Client, error) {
	srv, err := n.Server()
	if err != nil {
		return nil, err
	}
	return srv.HTTPClient(), nil
}

func (n *Node) logf(format string, args ...any) {
	if n.cfg.Logf != nil {
		n.cfg.Logf(format, args...)
	}
}
