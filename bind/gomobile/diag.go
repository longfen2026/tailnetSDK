// Copyright (c) tailnetSDK contributors
// SPDX-License-Identifier: BSD-3-Clause

package tailnetmobile

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"tailnetsdk/core"
)

// EventListener is invoked whenever an event occurs.
type EventListener interface {
	OnEvent(jsonEvent string)
}

// WatchSubscription represents an active event stream.
type WatchSubscription struct {
	cancel func()
}

// Close terminates the event stream.
func (s *WatchSubscription) Close() {
	if s != nil && s.cancel != nil {
		s.cancel()
	}
}

// Ping sends a TSMP ping to targetIP.
func (n *Node) Ping(targetIP string, timeoutMS int) (string, error) {
	cn, err := n.getNode()
	if err != nil {
		return "", err
	}
	to := defaultTimeout
	if timeoutMS > 0 {
		to = time.Duration(timeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), to)
	defer cancel()

	res, err := cn.Ping(ctx, targetIP)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WhoIs resolves node and user identity for remoteAddr.
func (n *Node) WhoIs(remoteAddr string) (string, error) {
	cn, err := n.getNode()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	ident, err := cn.WhoIs(ctx, remoteAddr)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(ident)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Watch subscribes to real-time events and passes JSON notifications to listener.
func (n *Node) Watch(listener EventListener) (*WatchSubscription, error) {
	if listener == nil {
		return nil, errors.New("listener cannot be nil")
	}
	cn, err := n.getNode()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopWatch, err := cn.Watch(ctx, func(ev core.Event) {
		b, err := json.Marshal(ev)
		if err == nil {
			listener.OnEvent(string(b))
		}
	})
	if err != nil {
		cancel()
		return nil, err
	}

	return &WatchSubscription{
		cancel: func() {
			stopWatch()
			cancel()
		},
	}, nil
}
