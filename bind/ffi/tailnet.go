// Copyright (c) tailnetSDK contributors
// SPDX-License-Identifier: BSD-3-Clause

// Package ffi exposes the tailnetSDK core as a plain C ABI so that any
// language with a C FFI can embed a tailnet node: C#/.NET via P/Invoke,
// Swift via the generated c-archive header, Kotlin via JNI, Python, Ruby...
//
// Build:
//
//	go build -buildmode=c-shared -o tailnet.dll .   (Windows shared library)
//	go build -buildmode=c-archive -o tailnet.a .    (static lib for iOS/macOS)
//
// The C surface mirrors tailscale.com/libtailscale where possible:
//
//   - nodes are referenced by opaque integer handles (never Go pointers);
//   - errors are returned as 0 / -errno plus a per-node last-error string;
//   - JSON documents are returned as malloc'ed C strings that the caller
//     must free();
//   - small values (IPs, state names, auth URLs) are copied into
//     caller-provided NUL-terminated buffers.
//
// Differences from libtailscale: dial/listen/event streams use 127.0.0.1
// TCP bridges instead of Unix socketpairs, because Go file descriptors are
// not CRT file descriptors on Windows and mobile platforms. See tunnel.go.
package main

//#include <errno.h>
//#include <stdlib.h>
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
	"unsafe"

	"tailnetsdk/core"
)

func main() {}

// defaultCtx bounds every SDK call made from the C surface, so a hung
// backend can never block the host UI thread forever.
const defaultCtxTimeout = 30 * time.Second

// node holds one core.Node plus the last error message reported for it.
type node struct {
	mu      sync.Mutex
	n       *core.Node
	lastErr string
}

func (m *node) recErr(err error) C.int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		m.lastErr = ""
		return 0
	}
	m.lastErr = err.Error()
	return -1
}

func (m *node) errText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}

// getNode looks up the handle. Lock ordering nodes.mu -> node.mu only.
var nodes = struct {
	mu   sync.Mutex
	next C.int
	m    map[C.int]*node
}{}

func getNode(sd C.int) *node {
	nodes.mu.Lock()
	defer nodes.mu.Unlock()
	return nodes.m[sd]
}

func deleteNode(sd C.int) *node {
	nodes.mu.Lock()
	defer nodes.mu.Unlock()
	n := nodes.m[sd]
	delete(nodes.m, sd)
	return n
}

// ffiConfig is the JSON schema accepted by tailnet_configure (camelCase,
// matching core.Config in docs/API.md).
type ffiConfig struct {
	Dir           string   `json:"dir"`
	Hostname      string   `json:"hostname"`
	ControlURL    string   `json:"controlURL"`
	Ephemeral     bool     `json:"ephemeral"`
	AuthKey       string   `json:"authKey"`
	EnableProxy   bool     `json:"enableProxy"`
	AdvertiseTags []string `json:"advertiseTags"`
}

// tailnet_new allocates a new node handle. Configure it with
// tailnet_configure before starting it.
//
//export tailnet_new
func tailnet_new() C.int {
	nodes.mu.Lock()
	defer nodes.mu.Unlock()
	if nodes.m == nil {
		nodes.m = map[C.int]*node{}
	}
	if nodes.next == 0 {
		nodes.next = 42<<16 + 1
	}
	sd := nodes.next
	nodes.next++
	nodes.m[sd] = &node{}
	return sd
}

// tailnet_configure applies a JSON configuration (see ffiConfig) and creates
// the underlying node. Call it once per handle, before tailnet_start.
//
//export tailnet_configure
func tailnet_configure(sd C.int, cfg *C.char) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	if cfg == nil {
		return m.recErr(fmt.Errorf("tailnet_configure: cfg is nil"))
	}
	var fc ffiConfig
	if err := json.Unmarshal([]byte(C.GoString(cfg)), &fc); err != nil {
		return m.recErr(fmt.Errorf("tailnet_configure: bad JSON: %v", err))
	}
	n, err := core.New(core.Config{
		Dir:           fc.Dir,
		Hostname:      fc.Hostname,
		ControlURL:    fc.ControlURL,
		Ephemeral:     fc.Ephemeral,
		AuthKey:       fc.AuthKey,
		EnableProxy:   fc.EnableProxy,
		AdvertiseTags: fc.AdvertiseTags,
	})
	if err != nil {
		return m.recErr(err)
	}
	m.mu.Lock()
	old := m.n
	m.n = n
	m.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return 0
}

// tailnet_start starts the node (joins the tailnet). The node may still be
// waiting for login; drive it with tailnet_login_url / tailnet_wait_running.
//
//export tailnet_start
func tailnet_start(sd C.int) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_start: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	return m.recErr(n.Start(ctx))
}

// tailnet_up starts the node and blocks until it is usable (authorized and
// connected), or timeout_ms elapses (returns -ETIMEDOUT).
//
//export tailnet_up
func tailnet_up(sd C.int, timeoutMS C.int) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_up: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout(timeoutMS))
	defer cancel()
	_, err := n.Up(ctx)
	return m.recErr(err)
}

// tailnet_close stops the node (if running), releases it and invalidates the
// handle. The handle must not be used afterwards.
//
//export tailnet_close
func tailnet_close(sd C.int) C.int {
	m := deleteNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return 0
	}
	return m.recErr(n.Close())
}

func (m *node) get() *core.Node {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.n
}

func ctxTimeout(ms C.int) time.Duration {
	if ms <= 0 {
		return defaultCtxTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

// copyString writes s into out (a caller-provided buffer) NUL-terminated.
// It returns 0 on success or C.ERANGE when the buffer is too small.
func copyString(out []byte, s string) C.int {
	if len(out) == 0 {
		return C.ERANGE
	}
	n := copy(out, s)
	if n >= len(out) {
		out[len(out)-1] = 0
		return C.ERANGE
	}
	out[n] = 0
	return 0
}
// ---------------------------------------------------------------------------
// Status, login and session management
// ---------------------------------------------------------------------------

// mallocJSON marshals v and returns a malloc'ed C string the caller must free
// with tailnet_free. It returns nil on marshaling or allocation failure.
func mallocJSON(v any) *C.char {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return C.CString(string(b))
}

// tailnet_free releases memory returned by the *_json / *_next_event calls.
// Must only be called on pointers obtained from this library.
//
//export tailnet_free
func tailnet_free(p *C.char) {
	if p != nil {
		C.free(unsafe.Pointer(p))
	}
}

// tailnet_status_json returns the full status snapshot (state, auth URL,
// device list, ...) as a malloc'ed JSON document. Free with tailnet_free.
//
//export tailnet_status_json
func tailnet_status_json(sd C.int, out **C.char) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_status_json: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	st, err := n.Status(ctx)
	if err != nil {
		return m.recErr(err)
	}
	*out = mallocJSON(st)
	if *out == nil {
		return m.recErr(fmt.Errorf("tailnet_status_json: unable to marshal status"))
	}
	return 0
}

// tailnet_peers_json returns just the device list as a malloc'ed JSON array.
//
//export tailnet_peers_json
func tailnet_peers_json(sd C.int, out **C.char) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_peers_json: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	peers, err := n.Peers(ctx)
	if err != nil {
		return m.recErr(err)
	}
	if peers == nil {
		peers = []core.Peer{}
	}
	*out = mallocJSON(peers)
	if *out == nil {
		return m.recErr(fmt.Errorf("tailnet_peers_json: unable to marshal peers"))
	}
	return 0
}

// tailnet_wait_running blocks until the node is authorized and connected (or
// needs admin approval, which returns -1 with ErrLoginRequired in
// tailnet_errmsg). On success *out receives a malloc'ed status JSON document.
//
//export tailnet_wait_running
func tailnet_wait_running(sd C.int, timeoutMS C.int, out **C.char) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_wait_running: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout(timeoutMS))
	defer cancel()
	st, err := n.WaitForRunning(ctx)
	if err != nil {
		return m.recErr(err)
	}
	*out = mallocJSON(st)
	if *out == nil {
		return m.recErr(fmt.Errorf("tailnet_wait_running: unable to marshal status"))
	}
	return 0
}

// tailnet_login_url copies the login URL (if any) into buf. Returns 0 with an
// empty string when the node does not need login. ERANGE means buf was too
// small.
//
//export tailnet_login_url
func tailnet_login_url(sd C.int, buf *C.char, n C.size_t) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	node := m.get()
	if node == nil {
		return m.recErr(fmt.Errorf("tailnet_login_url: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	url, err := node.LoginURL(ctx)
	if err != nil && url == "" && core.IsCode(err, core.ErrCodeBackend) {
		// "node is already authorized": report success with an empty URL so
		// hosts can treat an empty string as "no login needed".
		url = ""
	} else if err != nil {
		return m.recErr(err)
	}
	return copyString(unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(n)), url)
}

// tailnet_start_login_interactive asks the control plane for a fresh login
// URL; the URL is reported through the event stream and tailnet_login_url.
//
//export tailnet_start_login_interactive
func tailnet_start_login_interactive(sd C.int) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_start_login_interactive: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	return m.recErr(n.StartLoginInteractive(ctx))
}

// tailnet_logout deauthorizes the node and clears stored login state.
//
//export tailnet_logout
func tailnet_logout(sd C.int) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_logout: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	return m.recErr(n.Logout(ctx))
}

// tailnet_profiles_json returns the active login profile and all stored
// profiles as a malloc'ed JSON document: {"current":{...},"profiles":[...]}.
//
//export tailnet_profiles_json
func tailnet_profiles_json(sd C.int, out **C.char) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_profiles_json: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	cur, all, err := n.ProfileStatus(ctx)
	if err != nil {
		return m.recErr(err)
	}
	doc := struct {
		Current  *core.Profile  `json:"current"`
		Profiles []core.Profile `json:"profiles"`
	}{Current: cur, Profiles: all}
	*out = mallocJSON(doc)
	if *out == nil {
		return m.recErr(fmt.Errorf("tailnet_profiles_json: unable to marshal profiles"))
	}
	return 0
}

// tailnet_switch_profile activates the stored profile with the given ID.
//
//export tailnet_switch_profile
func tailnet_switch_profile(sd C.int, id *C.char) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	if id == nil {
		return m.recErr(fmt.Errorf("tailnet_switch_profile: id is nil"))
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_switch_profile: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	return m.recErr(n.SwitchProfile(ctx, C.GoString(id)))
}

// tailnet_delete_profile deletes the stored profile with the given ID.
//
//export tailnet_delete_profile
func tailnet_delete_profile(sd C.int, id *C.char) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	if id == nil {
		return m.recErr(fmt.Errorf("tailnet_delete_profile: id is nil"))
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_delete_profile: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	return m.recErr(n.DeleteProfile(ctx, C.GoString(id)))
}

// ---------------------------------------------------------------------------
// Tunnel: dial / listen / accept
//
// Instead of socketpairs (whose fd semantics differ between Windows CRT and
// Go), every connection is handed to the host as a one-shot 127.0.0.1 TCP
// bridge: the SDK returns an ephemeral loopback port, the host opens a single
// TCP connection to it, and the two streams are piped together. This works
// identically on Windows, macOS, iOS and Android.
// ---------------------------------------------------------------------------

// bridge serves exactly one host connection to 127.0.0.1:port and pipes it to
// tailnetConn until either side closes.
func bridge(tailnetConn net.Conn) (port C.int, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port = C.int(ln.Addr().(*net.TCPAddr).Port)
	go func() {
		defer ln.Close()
		host, aerr := ln.Accept()
		if aerr != nil {
			tailnetConn.Close()
			return
		}
		defer host.Close()
		defer tailnetConn.Close()
		done := make(chan struct{}, 2)
		go func() { _, _ = io.Copy(host, tailnetConn); done <- struct{}{} }()
		go func() { _, _ = io.Copy(tailnetConn, host); done <- struct{}{} }()
		<-done
	}()
	return port, nil
}

// listeners tracks tailnet_listen handles.
var listeners = struct {
	mu   sync.Mutex
	next C.int
	m    map[C.int]net.Listener
}{}

// tailnet_dial opens a tailnet connection to addr (MagicDNS name or tailnet
// IP with port) and returns the loopback bridge port in *port_out. The host
// opens ONE TCP connection to 127.0.0.1:<port>; closing it tears the tailnet
// connection down.
//
//export tailnet_dial
func tailnet_dial(sd C.int, network, addr *C.char, timeoutMS C.int, portOut *C.int) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	if network == nil || addr == nil || portOut == nil {
		return m.recErr(fmt.Errorf("tailnet_dial: nil argument"))
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_dial: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout(timeoutMS))
	defer cancel()
	c, err := n.Dial(ctx, C.GoString(network), C.GoString(addr))
	if err != nil {
		return m.recErr(err)
	}
	port, berr := bridge(c)
	if berr != nil {
		_ = c.Close()
		return m.recErr(fmt.Errorf("tailnet_dial: bridge: %w", berr))
	}
	*portOut = port
	return 0
}

// tailnet_listen starts listening on the tailnet (addr like ":8080") and
// returns a listener handle for tailnet_accept / tailnet_listener_close.
//
//export tailnet_listen
func tailnet_listen(sd C.int, network, addr *C.char, listenerOut *C.int) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	if network == nil || addr == nil || listenerOut == nil {
		return m.recErr(fmt.Errorf("tailnet_listen: nil argument"))
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_listen: node is not configured"))
	}
	ln, err := n.Listen(C.GoString(network), C.GoString(addr))
	if err != nil {
		return m.recErr(err)
	}
	listeners.mu.Lock()
	defer listeners.mu.Unlock()
	if listeners.m == nil {
		listeners.m = map[C.int]net.Listener{}
	}
	if listeners.next == 0 {
		listeners.next = 99 << 16
	}
	listeners.next++
	h := listeners.next
	listeners.m[h] = ln
	*listenerOut = h
	return 0
}

// tailnet_accept blocks until a peer connects to a tailnet listener, then
// returns a loopback bridge port for the host to pick the connection up.
// Call it once per incoming connection; close the host socket when done.
// tailnet_listener_close unblocks it with an error.
//
//export tailnet_accept
func tailnet_accept(lh C.int, portOut *C.int) C.int {
	listeners.mu.Lock()
	ln := listeners.m[lh]
	listeners.mu.Unlock()
	if ln == nil {
		return C.EBADF
	}
	if portOut == nil {
		return C.EINVAL
	}
	c, err := ln.Accept()
	if err != nil {
		return -C.EIO // listener closed or failed; use tailnet_listener_close
	}
	port, berr := bridge(c)
	if berr != nil {
		_ = c.Close()
		return -C.EIO
	}
	*portOut = port
	return 0
}

// tailnet_listener_close stops a tailnet listener and invalidates its handle.
//
//export tailnet_listener_close
func tailnet_listener_close(lh C.int) C.int {
	listeners.mu.Lock()
	ln := listeners.m[lh]
	delete(listeners.m, lh)
	listeners.mu.Unlock()
	if ln == nil {
		return C.EBADF
	}
	return C.int(errToRet(ln.Close()))
}

// errToRet maps an error to 0/-EIO for the few C exports without a node
// ---------------------------------------------------------------------------
// Proxy, local IPs, diagnostics and exit nodes
// ---------------------------------------------------------------------------

// tailnet_proxy_addrs starts (if needed) the loopback SOCKS5/HTTP proxy and
// copies its address and password into caller buffers. Any SOCKS5- or
// HTTP-proxy-aware HTTP stack in the host app can send traffic through the
// tailnet with this pair. ERANGE means a buffer was too small.
//
//export tailnet_proxy_addrs
func tailnet_proxy_addrs(sd C.int, addrBuf *C.char, addrN C.size_t, credBuf *C.char, credN C.size_t) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_proxy_addrs: node is not configured"))
	}
	addr, cred, ok := n.ProxyAddrs()
	if !ok {
		var err error
		addr, cred, err = n.StartProxy()
		if err != nil {
			return m.recErr(err)
		}
	}
	ret := copyString(unsafe.Slice((*byte)(unsafe.Pointer(addrBuf)), int(addrN)), addr)
	if ret != 0 {
		return ret
	}
	return copyString(unsafe.Slice((*byte)(unsafe.Pointer(credBuf)), int(credN)), cred)
}

// tailnet_getips copies the node's own tailnet IPv4 and IPv6 into caller
// buffers (empty strings while unauthorized).
//
//export tailnet_getips
func tailnet_getips(sd C.int, v4Buf *C.char, v4N C.size_t, v6Buf *C.char, v6N C.size_t) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_getips: node is not configured"))
	}
	v4, v6 := n.LocalIPs()
	if ret := copyString(unsafe.Slice((*byte)(unsafe.Pointer(v4Buf)), int(v4N)), v4.String()); ret != 0 {
		return ret
	}
	return copyString(unsafe.Slice((*byte)(unsafe.Pointer(v6Buf)), int(v6N)), v6.String())
}

// tailnet_ping_json runs a TSMP ping to a tailnet IP (connectivity
// diagnostic, "tailscale ping" equivalent) and returns the result as a
// malloc'ed JSON document.
//
//export tailnet_ping_json
func tailnet_ping_json(sd C.int, addr *C.char, out **C.char) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	if addr == nil {
		return m.recErr(fmt.Errorf("tailnet_ping_json: addr is nil"))
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_ping_json: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	res, err := n.Ping(ctx, C.GoString(addr))
	if err != nil {
		return m.recErr(err)
	}
	*out = mallocJSON(res)
	return 0
}

// tailnet_whois_json resolves the owner of an incoming tailnet connection
// ("ip:port" as returned by accept on the bridge or a tailnet listener) into
// a malloc'ed JSON identity document.
//
//export tailnet_whois_json
func tailnet_whois_json(sd C.int, remoteAddr *C.char, out **C.char) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	if remoteAddr == nil {
		return m.recErr(fmt.Errorf("tailnet_whois_json: addr is nil"))
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_whois_json: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	who, err := n.WhoIs(ctx, C.GoString(remoteAddr))
	if err != nil {
		return m.recErr(err)
	}
	*out = mallocJSON(who)
	return 0
}

// tailnet_set_exit_node routes this node's non-tailnet traffic through the
// exit node with the given stable ID (from the device list).
//
//export tailnet_set_exit_node
func tailnet_set_exit_node(sd C.int, nodeID *C.char) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	if nodeID == nil {
		return m.recErr(fmt.Errorf("tailnet_set_exit_node: node ID is nil"))
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_set_exit_node: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	return m.recErr(n.SetExitNode(ctx, C.GoString(nodeID)))
}

// tailnet_clear_exit_node stops using an exit node.
//
//export tailnet_clear_exit_node
func tailnet_clear_exit_node(sd C.int) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_clear_exit_node: node is not configured"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultCtxTimeout)
	defer cancel()
	return m.recErr(n.ClearExitNode(ctx))
}

// ---------------------------------------------------------------------------
// Event stream
// ---------------------------------------------------------------------------

// eventStream is one Watch subscription handed to the host.
type eventStream struct {
	ch   chan core.Event
	stop func()
}

var streams = struct {
	mu   sync.Mutex
	next C.int
	m    map[C.int]*eventStream
}{}

// tailnet_watch subscribes to node events (auth URLs, state transitions,
// login finished, self/peer changes). Returns a stream handle for
// tailnet_next_event / tailnet_stop_watch.
//
//export tailnet_watch
func tailnet_watch(sd C.int, streamOut *C.int) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	if streamOut == nil {
		return m.recErr(fmt.Errorf("tailnet_watch: streamOut is nil"))
	}
	n := m.get()
	if n == nil {
		return m.recErr(fmt.Errorf("tailnet_watch: node is not configured"))
	}
	st := &eventStream{ch: make(chan core.Event, 64)}
	stop, err := n.Watch(context.Background(), func(ev core.Event) {
		select {
		case st.ch <- ev:
		default: // never block the SDK's own event goroutine
		}
	})
	if err != nil {
		return m.recErr(err)
	}
	st.stop = stop
	streams.mu.Lock()
	defer streams.mu.Unlock()
	if streams.m == nil {
		streams.m = map[C.int]*eventStream{}
	}
	if streams.next == 0 {
		streams.next = 77 << 16
	}
	streams.next++
	h := streams.next
	streams.m[h] = st
	*streamOut = h
	return 0
}

// tailnet_next_event blocks until the next event arrives (returning it as a
// malloc'ed JSON document) or timeout_ms elapses (returning -ETIMEDOUT).
//
//export tailnet_next_event
func tailnet_next_event(sh C.int, timeoutMS C.int, out **C.char) C.int {
	streams.mu.Lock()
	st := streams.m[sh]
	streams.mu.Unlock()
	if st == nil {
		return C.EBADF
	}
	timeout := defaultCtxTimeout
	if timeoutMS > 0 {
		timeout = time.Duration(timeoutMS) * time.Millisecond
	}
	select {
	case ev := <-st.ch:
		*out = mallocJSON(ev)
		if *out == nil {
			return -C.EIO
		}
		return 0
	case <-time.After(timeout):
		return -C.ETIMEDOUT
	}
}

// tailnet_stop_watch cancels an event stream and invalidates its handle.
//
//export tailnet_stop_watch
func tailnet_stop_watch(sh C.int) C.int {
	streams.mu.Lock()
	st := streams.m[sh]
	delete(streams.m, sh)
	streams.mu.Unlock()
	if st == nil {
		return C.EBADF
	}
	st.stop()
	return 0
}

// ---------------------------------------------------------------------------
// Errors and version
// ---------------------------------------------------------------------------

// tailnet_errmsg copies the last error message of a node into buf. It is
// empty when the last call succeeded.
//
//export tailnet_errmsg
func tailnet_errmsg(sd C.int, buf *C.char, n C.size_t) C.int {
	m := getNode(sd)
	if m == nil {
		return C.EBADF
	}
	return copyString(unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(n)), m.errText())
}

// tailnet_version copies the SDK version into buf.
//
//export tailnet_version
func tailnet_version(buf *C.char, n C.size_t) C.int {
	return copyString(unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(n)), core.Version)
}


// handle to record lastErr on.
func errToRet(err error) int {
	if err == nil {
		return 0
	}
	return -1
}


