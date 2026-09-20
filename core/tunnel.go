package core

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
)

// Dial opens a connection to addr over the tailnet. addr may be a MagicDNS
// name ("host.tail-scale.ts.net:443"), a bare host name, or a tailnet IP.
//
// Only connections made through this method (or the listeners/proxy below)
// travel over the tailnet; the rest of the host app's traffic is untouched.
func (n *Node) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	srv, err := n.Server()
	if err != nil {
		return nil, err
	}
	if network == "" {
		network = "tcp"
	}
	if addr == "" {
		return nil, newError(ErrCodeInvalidArgument, "dial", "address must not be empty", nil)
	}
	c, err := srv.Dial(ctx, network, addr)
	if err != nil {
		return nil, newError(ErrCodeBackend, "dial", "unable to dial "+addr+" over the tailnet", err)
	}
	return c, nil
}

// Listen accepts connections from the tailnet on the node's own tailnet IP.
func (n *Node) Listen(network, addr string) (net.Listener, error) {
	srv, err := n.Server()
	if err != nil {
		return nil, err
	}
	if network == "" {
		network = "tcp"
	}
	ln, err := srv.Listen(network, addr)
	if err != nil {
		return nil, newError(ErrCodeBackend, "listen", "unable to listen on "+addr, err)
	}
	return ln, nil
}

// StartProxy starts the built-in loopback proxy and returns its address and
// the SOCKS5 password to use.
//
// The same address serves:
//   - SOCKS5 (username "tsnet", password = returned credential), which lets any
//     proxy-aware library or platform HTTP stack reach the tailnet;
//   - an HTTP proxy on the same port.
//
// The proxy only listens on loopback, so it is not reachable from the network.
func (n *Node) StartProxy() (addr, cred string, err error) {
	srv, err := n.Server()
	if err != nil {
		return "", "", err
	}
	addr, cred, _, err = srv.Loopback()
	if err != nil {
		return "", "", newError(ErrCodeBackend, "proxy", "unable to start the loopback tailnet proxy", err)
	}
	n.mu.Lock()
	n.proxyAddr, n.proxyCred = addr, cred
	n.mu.Unlock()
	return addr, cred, nil
}

// ProxyAddrs reports the loopback proxy address and credential, if a proxy is
// running (started either by Config.EnableProxy or by StartProxy).
func (n *Node) ProxyAddrs() (addr, cred string, ok bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.proxyAddr, n.proxyCred, n.proxyAddr != ""
}

// ServeProxy runs a TCP echo service over the tailnet until ctx is canceled.
// It is a convenience helper used by examples and the CLI smoke test.
func (n *Node) ServeProxy(ln net.Listener, handle func(net.Conn)) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, fs.ErrClosed) || errors.Is(err, os.ErrClosed) {
				return nil
			}
			return newError(ErrCodeBackend, "accept", "accept failed", err)
		}
		go handle(c)
	}
}
