package core

import (
	"strings"
	"time"
)

// Version is the version of this SDK.
const Version = "0.1.0"

// State mirrors the Tailscale backend state machine (ipn.State). It is
// represented as a string so that it survives every language binding and the
// JSON wire format unchanged.
type State string

const (
	StateNoState          State = "NoState"
	StateInUseOtherUser   State = "InUseOtherUser"
	StateNeedsLogin       State = "NeedsLogin"
	StateNeedsMachineAuth State = "NeedsMachineAuth"
	StateStopped          State = "Stopped"
	StateStarting         State = "Starting"
	StateRunning          State = "Running"
	StateUnknown          State = "Unknown"
)

// Running reports whether the node is connected to the tailnet and usable.
func (s State) Running() bool { return s == StateRunning }

// NeedsLogin reports whether an interactive login is required.
func (s State) NeedsLogin() bool { return s == StateNeedsLogin }

// Peer describes one device visible in the tailnet. The same type is used for
// the node itself (Status.Self) and for every peer.
type Peer struct {
	ID            string     `json:"id,omitempty"`
	NodeID        string     `json:"nodeID,omitempty"`
	HostName      string     `json:"hostName,omitempty"`
	DNSName       string     `json:"dnsName,omitempty"` // without the trailing dot
	OS            string     `json:"os,omitempty"`
	UserID        string     `json:"userID,omitempty"`
	IPs           []string   `json:"ips,omitempty"`
	Tags          []string   `json:"tags,omitempty"`
	Online        bool       `json:"online"`
	Active        bool       `json:"active"`
	Expired       bool       `json:"expired,omitempty"`
	ShareeNode    bool       `json:"shareeNode,omitempty"`
	Created       time.Time  `json:"created,omitempty"`
	LastSeen      time.Time  `json:"lastSeen,omitempty"`
	LastHandshake time.Time  `json:"lastHandshake,omitempty"`
	KeyExpiry     *time.Time `json:"keyExpiry,omitempty"`
}

// Status is the authorization and connectivity snapshot of the node. It is an
// SDK-owned type: upstream tailscale.com types never leak into the public API,
// so upstream refactors cannot break the language bindings.
type Status struct {
	State          State    `json:"state"`
	AuthURL        string   `json:"authURL,omitempty"`
	Version        string   `json:"version,omitempty"` // tailscale.com client version
	HaveNodeKey    bool     `json:"haveNodeKey,omitempty"`
	TUN            bool     `json:"tun"` // false = userspace networking, expected for this SDK
	TailnetName    string   `json:"tailnetName,omitempty"`
	MagicDNSSuffix string   `json:"magicDNSSuffix,omitempty"`
	MagicDNS       bool     `json:"magicDNS,omitempty"`
	TailscaleIPs   []string `json:"tailscaleIPs,omitempty"`
	Self           *Peer    `json:"self,omitempty"`
	Peers          []Peer   `json:"peers,omitempty"`
	CertDomains    []string `json:"certDomains,omitempty"`
	Health         []string `json:"health,omitempty"`
}

// OnlinePeerCount returns how many peers are currently connected.
func (s *Status) OnlinePeerCount() int {
	if s == nil {
		return 0
	}
	n := 0
	for _, p := range s.Peers {
		if p.Online {
			n++
		}
	}
	return n
}

// FindPeer returns the first peer whose host name, DNS name or Tailscale IP
// matches want (case-insensitive for names).
func (s *Status) FindPeer(want string) *Peer {
	if s == nil || want == "" {
		return nil
	}
	for i := range s.Peers {
		p := &s.Peers[i]
		if strings.EqualFold(p.HostName, want) || strings.EqualFold(p.DNSName, want) {
			return p
		}
		for _, ip := range p.IPs {
			if ip == want {
				return p
			}
		}
	}
	return nil
}

// Profile is a stored login profile (one tailnet identity).
type Profile struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	LoginName   string `json:"loginName,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	NodeID      string `json:"nodeID,omitempty"`
	ControlURL  string `json:"controlURL,omitempty"`
}

// Identity describes the owner of an incoming tailnet connection, as reported
// by Node.WhoIs.
type Identity struct {
	UserID        string   `json:"userID,omitempty"`
	LoginName     string   `json:"loginName,omitempty"`
	DisplayName   string   `json:"displayName,omitempty"`
	ProfilePicURL string   `json:"profilePicURL,omitempty"`
	NodeID        string   `json:"nodeID,omitempty"`
	NodeName      string   `json:"nodeName,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// PingResult is the outcome of Node.Ping.
type PingResult struct {
	IP             string  `json:"ip"`
	NodeIP         string  `json:"nodeIP,omitempty"`
	NodeName       string  `json:"nodeName,omitempty"`
	LatencySeconds float64 `json:"latencySeconds,omitempty"`
	Endpoint       string  `json:"endpoint,omitempty"`
	DERPRegionID   int     `json:"derpRegionID,omitempty"`
	DERPRegionCode string  `json:"derpRegionCode,omitempty"`
	Err            string  `json:"err,omitempty"`
}

// EventKind classifies a Node.Watch notification.
type EventKind string

const (
	// EventAuthURL carries a URL the user must open to authorize the node.
	EventAuthURL EventKind = "auth_url"
	// EventState carries a backend state transition.
	EventState EventKind = "state"
	// EventHealth carries the current health problems (empty = healthy).
	EventHealth EventKind = "health"
	// EventLoginFinished means authorization completed successfully.
	EventLoginFinished EventKind = "login_finished"
	// EventSelf carries the node's own identity after a change.
	EventSelf EventKind = "self"
	// EventPeers carries the current peer count after a netmap change.
	EventPeers EventKind = "peers"
	// EventError reports a failure of the notification stream itself.
	EventError EventKind = "error"
)

// Event is a single notification delivered to the callback registered with
// Node.Watch. It is JSON-serializable so that bindings can forward it to
// Kotlin/Swift/.NET without a bespoke ABI.
type Event struct {
	Kind      EventKind `json:"kind"`
	Time      time.Time `json:"time"`
	State     State     `json:"state,omitempty"`
	AuthURL   string    `json:"authURL,omitempty"`
	Health    []string  `json:"health,omitempty"`
	Self      *Peer     `json:"self,omitempty"`
	PeerCount int       `json:"peerCount,omitempty"`
	Message   string    `json:"message,omitempty"`
}
