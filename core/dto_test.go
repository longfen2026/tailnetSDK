package core

import (
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

func TestStateFromIPN(t *testing.T) {
	cases := []struct {
		in   ipn.State
		want State
	}{
		{ipn.NoState, StateNoState},
		{ipn.InUseOtherUser, StateInUseOtherUser},
		{ipn.NeedsLogin, StateNeedsLogin},
		{ipn.NeedsMachineAuth, StateNeedsMachineAuth},
		{ipn.Stopped, StateStopped},
		{ipn.Starting, StateStarting},
		{ipn.Running, StateRunning},
		{ipn.State(42), StateUnknown},
	}
	for _, tc := range cases {
		if got := stateFromIPN(tc.in); got != tc.want {
			t.Errorf("stateFromIPN(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMapStatusAndPeers(t *testing.T) {
	expiry := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	alpha := &ipnstate.PeerStatus{
		ID:       tailcfg.StableNodeID("n-aaa"),
		NodeID:   21,
		HostName: "alpha",
		DNSName:  "alpha.tail-scale.ts.net.",
		OS:       "macOS",
		TailscaleIPs: []netip.Addr{
			netip.MustParseAddr("100.64.0.2"),
		},
		Online: true,
		Active: true,
	}
	zulu := &ipnstate.PeerStatus{
		ID:       tailcfg.StableNodeID("n-bbb"),
		NodeID:   22,
		HostName: "zulu",
		DNSName:  "zulu.tail-scale.ts.net.",
		OS:       "linux",
		UserID:   tailcfg.UserID(99),
		TailscaleIPs: []netip.Addr{
			netip.MustParseAddr("100.64.0.3"),
		},
		Online:    false,
		KeyExpiry: &expiry,
	}
	st := &ipnstate.Status{
		Version:      "1.102.4",
		BackendState: "Running",
		TailscaleIPs: []netip.Addr{
			netip.MustParseAddr("100.64.0.1"),
			netip.MustParseAddr("fd7a:115c:a1e0::1"),
		},
		Self: &ipnstate.PeerStatus{
			ID:       tailcfg.StableNodeID("n-self"),
			NodeID:   1,
			HostName: "sdk-host",
			DNSName:  "sdk-host.tail-scale.ts.net.",
			OS:       "windows",
			Online:   true,
		},
		CurrentTailnet: &ipnstate.TailnetStatus{
			Name:            "example.com",
			MagicDNSSuffix:  "tail-scale.ts.net",
			MagicDNSEnabled: true,
		},
		Health: []string{"warning: something"},
		TUN:    false,
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			key.NewNode().Public(): zulu,
			key.NewNode().Public(): alpha,
		},
	}

	got := mapStatus(st)
	if got == nil {
		t.Fatal("mapStatus returned nil")
	}
	if got.State != StateRunning {
		t.Errorf("State = %q, want %q", got.State, StateRunning)
	}
	if got.TUN {
		t.Error("TUN = true, want false (this SDK uses userspace networking)")
	}
	if got.TailnetName != "example.com" || got.MagicDNSSuffix != "tail-scale.ts.net" || !got.MagicDNS {
		t.Errorf("tailnet info = %+v", got)
	}
	if len(got.TailscaleIPs) != 2 || got.TailscaleIPs[0] != "100.64.0.1" {
		t.Errorf("TailscaleIPs = %v", got.TailscaleIPs)
	}
	if got.Self == nil || got.Self.HostName != "sdk-host" {
		t.Fatalf("Self = %+v", got.Self)
	}
	if got.Self.DNSName != "sdk-host.tail-scale.ts.net" {
		t.Errorf("Self.DNSName = %q, want the trailing dot trimmed", got.Self.DNSName)
	}
	if len(got.Peers) != 2 {
		t.Fatalf("got %d peers, want 2", len(got.Peers))
	}
	if got.Peers[0].HostName != "alpha" || got.Peers[1].HostName != "zulu" {
		t.Errorf("peers are not sorted by DNS name: %q, %q", got.Peers[0].DNSName, got.Peers[1].DNSName)
	}
	if !got.Peers[0].Online || got.Peers[1].Online {
		t.Errorf("online flags = %v, %v", got.Peers[0].Online, got.Peers[1].Online)
	}
	if got.Peers[1].UserID != "99" {
		t.Errorf("UserID = %q, want 99", got.Peers[1].UserID)
	}
	if got.Peers[1].KeyExpiry == nil || !got.Peers[1].KeyExpiry.Equal(expiry) {
		t.Errorf("KeyExpiry = %v", got.Peers[1].KeyExpiry)
	}
	if got.OnlinePeerCount() != 1 {
		t.Errorf("OnlinePeerCount = %d, want 1", got.OnlinePeerCount())
	}
	if len(got.Health) != 1 {
		t.Errorf("Health = %v", got.Health)
	}
}

func TestMapStatusNilSafe(t *testing.T) {
	if got := mapStatus(nil); got != nil {
		t.Errorf("mapStatus(nil) = %+v, want nil", got)
	}
	if got := mapPeer(nil); got != nil {
		t.Errorf("mapPeer(nil) = %+v, want nil", got)
	}
	if got := addrsToStrings(nil); got != nil {
		t.Errorf("addrsToStrings(nil) = %v, want nil", got)
	}
	if got := addrsToStrings([]netip.Addr{{}}); got != nil {
		t.Errorf("addrsToStrings(invalid) = %v, want nil", got)
	}
}

func TestFindPeer(t *testing.T) {
	st := &Status{
		Peers: []Peer{
			{HostName: "Alpha", DNSName: "alpha.example.ts.net", IPs: []string{"100.64.0.2"}},
			{HostName: "beta", DNSName: "beta.example.ts.net", IPs: []string{"100.64.0.3"}},
		},
	}
	if p := st.FindPeer("alpha"); p == nil || p.HostName != "Alpha" {
		t.Errorf("FindPeer(alpha) = %+v", p)
	}
	if p := st.FindPeer("ALPHA.EXAMPLE.TS.NET"); p == nil {
		t.Error("FindPeer should match DNS names case-insensitively")
	}
	if p := st.FindPeer("100.64.0.3"); p == nil || p.HostName != "beta" {
		t.Errorf("FindPeer(by IP) = %+v", p)
	}
	if p := st.FindPeer("nope"); p != nil {
		t.Errorf("FindPeer(nope) = %+v, want nil", p)
	}
	var nilStatus *Status
	if p := nilStatus.FindPeer("alpha"); p != nil {
		t.Errorf("FindPeer on a nil status = %+v", p)
	}
}

func TestEventJSON(t *testing.T) {
	ev := Event{
		Kind:    EventAuthURL,
		Time:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		AuthURL: "https://login.tailscale.com/a/deadbeef",
		Health:  []string{},
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var back Event
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if back.Kind != EventAuthURL || back.AuthURL != ev.AuthURL || !back.Time.Equal(ev.Time) {
		t.Errorf("round trip = %+v, want %+v", back, ev)
	}
}

func TestConfigDefaults(t *testing.T) {
	got, err := Config{}.withDefaults()
	if err != nil {
		t.Fatalf("withDefaults: %v", err)
	}
	if got.Hostname != DefaultHostname {
		t.Errorf("Hostname = %q, want %q", got.Hostname, DefaultHostname)
	}
	if got.ControlURL != DefaultControlURL {
		t.Errorf("ControlURL = %q, want %q", got.ControlURL, DefaultControlURL)
	}
	if got.Dir == "" {
		t.Error("Dir should have been derived")
	}

	rel, err := Config{Dir: "relative-dir", ControlURL: "https://control.example.com"}.withDefaults()
	if err != nil {
		t.Fatalf("withDefaults: %v", err)
	}
	if !filepath.IsAbs(rel.Dir) {
		t.Errorf("Dir = %q, want an absolute path", rel.Dir)
	}
	if rel.ControlURL != "https://control.example.com" {
		t.Errorf("ControlURL = %q, want the override to be preserved", rel.ControlURL)
	}
}

func TestErrorCode(t *testing.T) {
	err := newError(ErrCodeBackend, "dial", "boom", io.EOF)
	if Code(err) != ErrCodeBackend {
		t.Errorf("Code = %q", Code(err))
	}
	if !IsCode(err, ErrCodeBackend) {
		t.Error("IsCode should match")
	}
	if !errors.Is(err, io.EOF) {
		t.Error("the wrapped cause should be unwrappable")
	}
	if Code(nil) != "" {
		t.Error("Code(nil) should be empty")
	}
	if Code(errors.New("plain")) != ErrCodeUnknown {
		t.Error("foreign errors should map to ErrCodeUnknown")
	}
	if !StateRunning.Running() || StateNeedsLogin.Running() {
		t.Error("State.Running() is wrong")
	}
	if !StateNeedsLogin.NeedsLogin() {
		t.Error("State.NeedsLogin() is wrong")
	}
}
