// Copyright (c) tailnetSDK contributors
// SPDX-License-Identifier: BSD-3-Clause

// Package tailnetmobile wraps the tailnetSDK core for mobile environments
// (Android / iOS) via gomobile bind.
package tailnetmobile

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"tailnetsdk/core"
)

const defaultTimeout = 30 * time.Second

// Node embeds a tailnet node inside the mobile host app.
type Node struct {
	mu        sync.Mutex
	coreNode  *core.Node
	bridges   map[int]string
	bridgesMu sync.RWMutex
}

// NewNode constructs an unconfigured tailnet node.
func NewNode() *Node {
	return &Node{
		bridges: make(map[int]string),
	}
}

// Configure configures and initializes the node before Start.
func (n *Node) Configure(dir, hostname, controlURL, authKey string, ephemeral bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	cfg := core.Config{
		Dir:         dir,
		Hostname:    hostname,
		ControlURL:  controlURL,
		AuthKey:     authKey,
		Ephemeral:   ephemeral,
		EnableProxy: true,
	}

	coreNode, err := core.New(cfg)
	if err != nil {
		return err
	}

	if n.coreNode != nil {
		_ = n.coreNode.Close()
	}
	n.coreNode = coreNode
	return nil
}

func (n *Node) getNode() (*core.Node, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.coreNode == nil {
		return nil, errors.New("node is not configured; call Configure first")
	}
	return n.coreNode, nil
}

// Start boots the user-space tailnet engine.
func (n *Node) Start() error {
	cn, err := n.getNode()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	return cn.Start(ctx)
}

// Close stops the engine and releases all allocated bridges.
func (n *Node) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.coreNode != nil {
		return n.coreNode.Close()
	}
	return nil
}

// State returns the current IPN state.
func (n *Node) State() string {
	cn, err := n.getNode()
	if err != nil {
		return "Unknown"
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	st, err := cn.Status(ctx)
	if err != nil {
		return "NoState"
	}
	return string(st.State)
}

// TailnetIP returns the first IPv4 address assigned to the node on the tailnet.
func (n *Node) TailnetIP() string {
	cn, err := n.getNode()
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	st, err := cn.Status(ctx)
	if err != nil || len(st.TailscaleIPs) == 0 {
		return ""
	}
	return st.TailscaleIPs[0]
}

// AuthURL returns the active interactive login URL, if any.
func (n *Node) AuthURL() (string, error) {
	cn, err := n.getNode()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	return cn.LoginURL(ctx)
}

// StartLoginInteractive triggers an interactive login flow.
func (n *Node) StartLoginInteractive() error {
	cn, err := n.getNode()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	return cn.StartLoginInteractive(ctx)
}

// Logout disconnects and clears the stored session credentials.
func (n *Node) Logout() error {
	cn, err := n.getNode()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	return cn.Logout(ctx)
}

// StatusJSON returns the full NodeStatus DTO serialized as JSON.
func (n *Node) StatusJSON() (string, error) {
	cn, err := n.getNode()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	st, err := cn.Status(ctx)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// PeersJSON returns the list of tailnet peers serialized as JSON.
func (n *Node) PeersJSON() (string, error) {
	cn, err := n.getNode()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	peers, err := cn.Peers(ctx)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(peers)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// StartProxy starts the local loopback proxy (SOCKS5 + HTTP).
func (n *Node) StartProxy() (string, error) {
	cn, err := n.getNode()
	if err != nil {
		return "", err
	}
	addr, _, err := cn.StartProxy()
	return addr, err
}

// SOCKS5Addr returns the active loopback proxy address.
func (n *Node) SOCKS5Addr() string {
	cn, err := n.getNode()
	if err != nil {
		return ""
	}
	addr, _, ok := cn.ProxyAddrs()
	if !ok {
		return ""
	}
	return addr
}
