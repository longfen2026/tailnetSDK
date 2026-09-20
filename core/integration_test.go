package core

// Integration tests. They run a full local control plane (tailscale.com
// tstest/integration/testcontrol) plus a local DERP/STUN relay in-process, so
// the whole login -> running -> tunnel loop is exercised without any network
// access or Tailscale credentials. This is the M1 acceptance suite: CI can
// regress it with no real tailnet account.
//
// Run:  go test ./core/ -run TestIntegration -count=1 -timeout 15m
// Skip: go test -short ./core/   (integration tests are skipped)

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"tailscale.com/net/netns"
	"tailscale.com/tailcfg"
	"tailscale.com/tstest/integration"
	"tailscale.com/tstest/integration/testcontrol"
	"tailscale.com/types/logger"
)

const itestTimeout = 90 * time.Second

// startTestControl boots a local control plane (and DERP/STUN relay) and
// returns the ControlURL nodes should use to reach it. The testcontrol server
// auto-approves every registering node, which is what makes the whole
// login flow verifiable without a browser or credentials.
func startTestControl(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	// Corp#4520: don't let netns interfere with the tests.
	netns.SetEnabled(false)
	t.Cleanup(func() { netns.SetEnabled(true) })

	derpMap := integration.RunDERPAndSTUN(t, logger.Discard, "127.0.0.1")
	control := &testcontrol.Server{
		DERPMap:        derpMap,
		DNSConfig:      &tailcfg.DNSConfig{Proxied: true},
		MagicDNSDomain: "tail-scale.ts.net",
		Logf:           logger.Discard,
	}
	control.HTTPTestServer = httptest.NewUnstartedServer(control)
	control.HTTPTestServer.Start()
	t.Cleanup(control.HTTPTestServer.Close)
	return control.HTTPTestServer.URL
}

// newTestNode creates an ephemeral SDK node pointed at the local control plane.
func newTestNode(t *testing.T, controlURL, hostname string) *Node {
	t.Helper()
	node, err := New(Config{
		Dir:        t.TempDir(),
		Hostname:   hostname,
		ControlURL: controlURL,
		Ephemeral:  true,
	})
	if err != nil {
		t.Fatalf("New(%s): %v", hostname, err)
	}
	t.Cleanup(func() { node.Close() })
	return node
}

// startNode starts the node and blocks until it is Running (the local control
// plane completes the login automatically).
func startNode(t *testing.T, node *Node) *Status {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), itestTimeout)
	t.Cleanup(cancel)
	if err := node.Start(ctx); err != nil {
		t.Fatalf("Start(%s): %v", node.Config().Hostname, err)
	}
	st, err := node.WaitForRunning(ctx)
	if err != nil {
		t.Fatalf("WaitForRunning(%s): %v", node.Config().Hostname, err)
	}
	return st
}

// waitForPeer polls node's device list until a peer named hostname shows up.
func waitForPeer(t *testing.T, node *Node, hostname string, timeout time.Duration) Peer {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *Status
	for {
		if st, err := node.Status(context.Background()); err == nil {
			last = st
			if p := st.FindPeer(hostname); p != nil {
				return *p
			}
		}
		if time.Now().After(deadline) {
			n := 0
			if last != nil {
				n = len(last.Peers)
			}
			t.Fatalf("peer %q did not appear within %v (last peer count: %d)", hostname, timeout, n)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// echoStats records what the echo listener observed, for failure diagnosis.
type echoStats struct {
	mu       sync.Mutex
	accepted int
	whoisErr string
	whoisID  *Identity
	echoed   int
	echoErr  string
}

func (s *echoStats) dump() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("accepted=%d whoisErr=%q echoed=%d echoErr=%q", s.accepted, s.whoisErr, s.echoed, s.echoErr)
}

// startEchoListener makes node listen on port and echo every connection,
// recording the WhoIs identity of each caller. The WhoIs lookup is isolated
// with its own short timeout so a slow lookup cannot delay the echo.
func startEchoListener(t *testing.T, node *Node, port string, st *echoStats) {
	t.Helper()
	ln, err := node.Listen("tcp", ":"+port)
	if err != nil {
		t.Fatalf("Listen(:%s): %v", port, err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				st.mu.Lock()
				st.accepted++
				st.mu.Unlock()

				whoCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				who, whoErr := node.WhoIs(whoCtx, c.RemoteAddr().String())
				cancel()
				st.mu.Lock()
				if whoErr != nil {
					st.whoisErr = whoErr.Error()
				}
				st.whoisID = who
				st.mu.Unlock()

				// Echo chunk by chunk so the stats reflect progress while the
				// connection is still open (io.Copy only reports on EOF).
				buf := make([]byte, 4096)
				for {
					n, rerr := c.Read(buf)
					if n > 0 {
						if _, werr := c.Write(buf[:n]); werr != nil {
							st.mu.Lock()
							st.echoErr = werr.Error()
							st.mu.Unlock()
							return
						}
						st.mu.Lock()
						st.echoed += n
						st.mu.Unlock()
					}
					if rerr != nil {
						if !errors.Is(rerr, io.EOF) {
							st.mu.Lock()
							st.echoErr = rerr.Error()
							st.mu.Unlock()
						}
						return
					}
				}
			}(c)
		}
	}()
}

// startHTTPListener serves a fixed HTTP response body on node's tailnet listener.
func startHTTPListener(t *testing.T, node *Node, port, body string) {
	t.Helper()
	ln, err := node.Listen("tcp", ":"+port)
	if err != nil {
		t.Fatalf("Listen(:%s): %v", port, err)
	}
	hs := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	})}
	go func() { _ = hs.Serve(ln) }()
	t.Cleanup(func() {
		_ = hs.Close()
		_ = ln.Close()
	})
}

// waitReachable blocks until from can TSMP-ping to, making sure the WireGuard
// path between the two nodes is established before any TCP dial. This mirrors
// the upstream tsnet tests, which ping between nodes before passing TCP data
// to avoid racing the DERP/DISCO handshake (dropped frames otherwise show up
// as a connection that is established but never carries data).
func waitReachable(t *testing.T, from, to *Node) {
	t.Helper()
	st, err := to.StatusWithoutPeers(context.Background())
	if err != nil {
		t.Fatalf("StatusWithoutPeers(%s): %v", to.Config().Hostname, err)
	}
	if len(st.TailscaleIPs) == 0 {
		t.Fatalf("%s has no tailscale IP yet", to.Config().Hostname)
	}
	target := st.TailscaleIPs[0]
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, lastErr = from.Ping(ctx, target)
		cancel()
		if lastErr == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s cannot ping %s (%s) within 30s: %v", from.Config().Hostname, to.Config().Hostname, target, lastErr)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func TestIntegrationAuthLifecycle(t *testing.T) {
	controlURL := startTestControl(t)
	node := newTestNode(t, controlURL, "sdk-a")

	ctx, cancel := context.WithTimeout(context.Background(), itestTimeout)
	defer cancel()
	if err := node.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st, err := node.WaitForRunning(ctx)
	if err != nil {
		t.Fatalf("WaitForRunning: %v", err)
	}
	if st.State != StateRunning {
		t.Errorf("state = %q, want Running", st.State)
	}
	if st.TUN {
		t.Error("Status.TUN = true, want false (userspace mode)")
	}
	if len(st.TailscaleIPs) == 0 || !strings.HasPrefix(st.TailscaleIPs[0], "100.") {
		t.Errorf("TailscaleIPs = %v, want a 100.64.0.0/10 address", st.TailscaleIPs)
	}
	if st.Self == nil || st.Self.HostName == "" {
		t.Errorf("Status.Self missing or unnamed: %+v", st.Self)
	}
	if st.TailnetName == "" {
		t.Error("Status.TailnetName is empty")
	}

	// Once authorized, LoginURL must not hand out a URL: it reports a backend
	// error ("node is already authorized") so hosts can branch on error codes.
	if url, err := node.LoginURL(ctx); err == nil {
		t.Errorf("LoginURL after Running returned %q without error", url)
	} else if !IsCode(err, ErrCodeBackend) {
		t.Errorf("LoginURL after Running returned code %q, want %q (err: %v)", Code(err), ErrCodeBackend, err)
	}

	// The profile recorded the control URL override.
	cur, _, err := node.ProfileStatus(ctx)
	if err != nil {
		t.Fatalf("ProfileStatus: %v", err)
	}
	if cur == nil || cur.ControlURL != controlURL {
		t.Errorf("profile ControlURL = %+v, want %q", cur, controlURL)
	}
}

func TestIntegrationPeerDiscovery(t *testing.T) {
	controlURL := startTestControl(t)
	a := newTestNode(t, controlURL, "sdk-a")
	b := newTestNode(t, controlURL, "sdk-b")

	startNode(t, a)
	startNode(t, b)

	// Each node must see the other with a tailnet IP and DNS name.
	pa := waitForPeer(t, a, "sdk-b", 30*time.Second)
	if len(pa.IPs) == 0 || !strings.HasPrefix(pa.IPs[0], "100.") {
		t.Errorf("sdk-a sees sdk-b with IPs %v, want a 100.64.0.0/10 address", pa.IPs)
	}
	if pa.DNSName == "" {
		t.Error("sdk-a sees sdk-b without a DNS name")
	}
	pb := waitForPeer(t, b, "sdk-a", 30*time.Second)
	if len(pb.IPs) == 0 {
		t.Error("sdk-b sees sdk-a without tailscale IPs")
	}

	// OnlinePeerCount / FindPeer helpers behave on the full status.
	st, err := a.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.OnlinePeerCount() > len(st.Peers) {
		t.Errorf("OnlinePeerCount = %d > len(Peers) = %d", st.OnlinePeerCount(), len(st.Peers))
	}
}

func TestIntegrationTunnelEcho(t *testing.T) {
	controlURL := startTestControl(t)
	a := newTestNode(t, controlURL, "sdk-a")
	b := newTestNode(t, controlURL, "sdk-b")

	startNode(t, a)
	startNode(t, b)
	// Both directions must know each other before dialing, so the DERP
	// handshake has settled and DISCO frames are not dropped.
	waitForPeer(t, a, "sdk-b", 30*time.Second)
	waitForPeer(t, b, "sdk-a", 30*time.Second)
	// Warm up the WireGuard path in both directions (mirrors upstream tests,
	// which ping before passing TCP data).
	waitReachable(t, a, b)
	waitReachable(t, b, a)

	whoStats := &echoStats{}
	startEchoListener(t, a, "8080", whoStats)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// MagicDNS short name: b reaches a without knowing its tailnet IP.
	conn, err := b.Dial(ctx, "tcp", "sdk-a:8080")
	if err != nil {
		t.Fatalf("Dial(sdk-a:8080): %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	const msg = "hello over tailnet"
	if _, err := io.WriteString(conn, msg+"\n"); err != nil {
		t.Fatalf("write: %v (echo stats: %s)", err, whoStats.dump())
	}
	// The echo listener keeps the connection open, so read exactly one echo
	// (len(msg) bytes) instead of waiting for EOF.
	reply := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read echo: %v (echo stats: %s)", err, whoStats.dump())
	}
	if got := string(reply); got != msg {
		t.Errorf("echo = %q, want %q (echo stats: %s)", got, msg, whoStats.dump())
	}

	// The listener side must be able to attribute the connection.
	whoStats.mu.Lock()
	gotID := whoStats.whoisID
	whoStats.mu.Unlock()
	if gotID == nil {
		t.Errorf("WhoIs did not resolve the dialing node (echo stats: %s)", whoStats.dump())
	} else if gotID.LoginName == "" && gotID.NodeName == "" {
		t.Errorf("WhoIs identity is empty: %+v", gotID)
	}
}

func TestIntegrationHTTPClientOverTailnet(t *testing.T) {
	controlURL := startTestControl(t)
	a := newTestNode(t, controlURL, "sdk-a")
	b := newTestNode(t, controlURL, "sdk-b")

	startNode(t, a)
	startNode(t, b)
	waitForPeer(t, a, "sdk-b", 30*time.Second)
	waitForPeer(t, b, "sdk-a", 30*time.Second)
	waitReachable(t, b, a) // warm up the WireGuard path before the HTTP dial

	startHTTPListener(t, a, "8090", "tailnet hello")

	client, err := b.HTTPClient()
	if err != nil {
		t.Fatalf("HTTPClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://sdk-a:8090/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET http://sdk-a:8090/: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if strings.TrimSpace(string(body)) != "tailnet hello" {
		t.Errorf("body = %q, want %q", body, "tailnet hello")
	}
}

func TestIntegrationSOCKS5Proxy(t *testing.T) {
	controlURL := startTestControl(t)
	a := newTestNode(t, controlURL, "sdk-a")
	b := newTestNode(t, controlURL, "sdk-b")

	startNode(t, a)
	startNode(t, b)
	waitForPeer(t, a, "sdk-b", 30*time.Second)
	waitForPeer(t, b, "sdk-a", 30*time.Second)
	waitReachable(t, b, a) // warm up the WireGuard path before the proxied dial

	startHTTPListener(t, a, "8091", "proxy hello")

	// The loopback proxy on b lets any (non-Go) HTTP stack reach the tailnet.
	addr, cred, err := b.StartProxy()
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Errorf("proxy addr = %q, want a loopback address", addr)
	}
	a2, c2, ok := b.ProxyAddrs()
	if !ok || a2 != addr || c2 != cred {
		t.Errorf("ProxyAddrs = (%q, %q, %v), want (%q, %q, true)", a2, c2, ok, addr, cred)
	}

	dialer, err := proxy.SOCKS5("tcp", addr, &proxy.Auth{User: "tsnet", Password: cred}, proxy.Direct)
	if err != nil {
		t.Fatalf("SOCKS5 dialer: %v", err)
	}
	client := &http.Client{
		Transport: &http.Transport{Dial: dialer.Dial},
		Timeout:   30 * time.Second,
	}
	resp, err := client.Get("http://sdk-a:8091/")
	if err != nil {
		t.Fatalf("GET via SOCKS5: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.TrimSpace(string(body)) != "proxy hello" {
		t.Errorf("body = %q, want %q", body, "proxy hello")
	}
}

func TestIntegrationLogout(t *testing.T) {
	controlURL := startTestControl(t)
	node := newTestNode(t, controlURL, "sdk-a")
	startNode(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := node.Logout(ctx); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	// After logout the backend must leave Running at least briefly. tsnet's
	// automatic login loop re-authorizes against the local control plane
	// within seconds, so we only require one non-Running observation.
	deadline := time.Now().Add(10 * time.Second)
	sawLoggedOut := false
	for time.Now().Before(deadline) {
		if st, err := node.StatusWithoutPeers(ctx); err == nil && !st.State.Running() {
			sawLoggedOut = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !sawLoggedOut {
		t.Error("backend state stayed Running for 10s after Logout")
	}
}

func TestIntegrationWatchEvents(t *testing.T) {
	controlURL := startTestControl(t)
	node := newTestNode(t, controlURL, "sdk-a")

	ctx, cancel := context.WithTimeout(context.Background(), itestTimeout)
	defer cancel()
	if err := node.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var (
		mu    sync.Mutex
		kinds []EventKind
	)
	stop, err := node.Watch(ctx, func(ev Event) {
		mu.Lock()
		defer mu.Unlock()
		kinds = append(kinds, ev.Kind)
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if _, err := node.WaitForRunning(ctx); err != nil {
		t.Fatalf("WaitForRunning: %v", err)
	}

	// Give the bus a moment to deliver at least one state/self/peers event.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(kinds)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	sawCore := false
	for _, k := range kinds {
		if k == EventState || k == EventSelf || k == EventPeers {
			sawCore = true
		}
	}
	copied := append([]EventKind(nil), kinds...)
	mu.Unlock()
	if !sawCore {
		t.Errorf("no state/self/peers events observed within 15s; got %v", copied)
	}

	// stop must be safe to call and safe to call twice.
	stop()
	stop()
}
