// Copyright (c) tailnetSDK contributors
// SPDX-License-Identifier: BSD-3-Clause

package tailnetmobile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Listener wraps a tailnet listener exposed over loopback TCP bridges.
type Listener struct {
	mu        sync.Mutex
	nl        net.Listener
	closed    bool
	nodeOwner *Node
}

// Accept waits for a new tailnet connection and exposes it on a 127.0.0.1 TCP port.
// The host app can connect via standard java.net.Socket("127.0.0.1", port).
func (l *Listener) Accept() (int, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return 0, errors.New("listener closed")
	}
	nl := l.nl
	l.mu.Unlock()

	conn, err := nl.Accept()
	if err != nil {
		return 0, err
	}

	return bridgeSingleConn(conn, l.nodeOwner)
}

// Close closes the tailnet listener.
func (l *Listener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return l.nl.Close()
}

// Dial establishes a tailnet connection and exposes it on a local loopback TCP port.
func (n *Node) Dial(network, address string, timeoutMS int) (int, error) {
	cn, err := n.getNode()
	if err != nil {
		return 0, err
	}
	to := defaultTimeout
	if timeoutMS > 0 {
		to = time.Duration(timeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), to)
	defer cancel()

	remoteConn, err := cn.Dial(ctx, network, address)
	if err != nil {
		return 0, err
	}

	return bridgeSingleConn(remoteConn, n)
}

// Listen creates a tailnet listener.
func (n *Node) Listen(network, address string) (*Listener, error) {
	cn, err := n.getNode()
	if err != nil {
		return nil, err
	}
	nl, err := cn.Listen(network, address)
	if err != nil {
		return nil, err
	}
	return &Listener{
		nl:        nl,
		nodeOwner: n,
	}, nil
}

// RemoteTailnetAddr returns the original peer tailnet IP:port behind bridgePort.
func (n *Node) RemoteTailnetAddr(bridgePort int) string {
	n.bridgesMu.RLock()
	defer n.bridgesMu.RUnlock()
	return n.bridges[bridgePort]
}

func (n *Node) recordBridge(port int, remoteAddr string) {
	n.bridgesMu.Lock()
	defer n.bridgesMu.Unlock()
	n.bridges[port] = remoteAddr
}

func (n *Node) removeBridge(port int) {
	n.bridgesMu.Lock()
	defer n.bridgesMu.Unlock()
	delete(n.bridges, port)
}

func bridgeSingleConn(remoteConn net.Conn, owner *Node) (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = remoteConn.Close()
		return 0, fmt.Errorf("create loopback bridge: %w", err)
	}

	port := l.Addr().(*net.TCPAddr).Port
	if owner != nil && remoteConn.RemoteAddr() != nil {
		owner.recordBridge(port, remoteConn.RemoteAddr().String())
	}

	go func() {
		defer l.Close()
		defer func() {
			if owner != nil {
				owner.removeBridge(port)
			}
		}()

		localConn, err := l.Accept()
		if err != nil {
			_ = remoteConn.Close()
			return
		}
		defer localConn.Close()
		defer remoteConn.Close()

		var wg sync.WaitGroup
		wg.Add(2)
		pipe := func(dst io.Writer, src io.Reader) {
			defer wg.Done()
			_, _ = io.Copy(dst, src)
		}
		go pipe(localConn, remoteConn)
		go pipe(remoteConn, localConn)
		wg.Wait()
	}()

	return port, nil
}
