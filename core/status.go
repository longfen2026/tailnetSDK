package core

import (
	"context"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
)

// Status returns the full authorization and connectivity snapshot, including
// the device list (Status.Peers).
func (n *Node) Status(ctx context.Context) (*Status, error) {
	lc, err := n.LocalClient()
	if err != nil {
		return nil, err
	}
	st, err := lc.Status(ctx)
	if err != nil {
		return nil, newError(ErrCodeBackend, "status", "unable to read node status", err)
	}
	return mapStatus(st), nil
}

// StatusWithoutPeers is like Status but skips the device list. It is cheaper
// and is the right call when polling.
func (n *Node) StatusWithoutPeers(ctx context.Context) (*Status, error) {
	lc, err := n.LocalClient()
	if err != nil {
		return nil, err
	}
	st, err := lc.StatusWithoutPeers(ctx)
	if err != nil {
		return nil, newError(ErrCodeBackend, "status", "unable to read node status", err)
	}
	return mapStatus(st), nil
}

// Peers returns just the device list of the tailnet, sorted by DNS name.
func (n *Node) Peers(ctx context.Context) ([]Peer, error) {
	st, err := n.Status(ctx)
	if err != nil {
		return nil, err
	}
	return st.Peers, nil
}

// stateFromIPN converts the upstream ipn.State enum into the SDK State.
func stateFromIPN(s ipn.State) State {
	switch s {
	case ipn.NoState:
		return StateNoState
	case ipn.InUseOtherUser:
		return StateInUseOtherUser
	case ipn.NeedsLogin:
		return StateNeedsLogin
	case ipn.NeedsMachineAuth:
		return StateNeedsMachineAuth
	case ipn.Stopped:
		return StateStopped
	case ipn.Starting:
		return StateStarting
	case ipn.Running:
		return StateRunning
	default:
		return StateUnknown
	}
}

// mapStatus converts the upstream status into the SDK DTO. Upstream types are
// deliberately kept out of the public API so that upgrading tailscale.com only
// requires touching this file.
func mapStatus(st *ipnstate.Status) *Status {
	if st == nil {
		return nil
	}
	out := &Status{
		State:        State(st.BackendState),
		AuthURL:      st.AuthURL,
		Version:      st.Version,
		HaveNodeKey:  st.HaveNodeKey,
		TUN:          st.TUN,
		TailscaleIPs: addrsToStrings(st.TailscaleIPs),
		Self:         mapPeer(st.Self),
		CertDomains:  st.CertDomains,
		Health:       st.Health,
	}
	if st.CurrentTailnet != nil {
		out.TailnetName = st.CurrentTailnet.Name
		out.MagicDNSSuffix = st.CurrentTailnet.MagicDNSSuffix
		out.MagicDNS = st.CurrentTailnet.MagicDNSEnabled
	}
	if len(st.Peer) > 0 {
		peers := make([]Peer, 0, len(st.Peer))
		for _, p := range st.Peer {
			if pp := mapPeer(p); pp != nil {
				peers = append(peers, *pp)
			}
		}
		sort.Slice(peers, func(i, j int) bool {
			if peers[i].DNSName == peers[j].DNSName {
				return peers[i].HostName < peers[j].HostName
			}
			return peers[i].DNSName < peers[j].DNSName
		})
		out.Peers = peers
	}
	return out
}

// mapPeer converts one upstream peer status into the SDK DTO.
func mapPeer(p *ipnstate.PeerStatus) *Peer {
	if p == nil {
		return nil
	}
	out := &Peer{
		ID:            string(p.ID),
		NodeID:        strconv.FormatInt(int64(p.NodeID), 10),
		HostName:      p.HostName,
		DNSName:       strings.TrimSuffix(p.DNSName, "."),
		OS:            p.OS,
		UserID:        strconv.FormatInt(int64(p.UserID), 10),
		IPs:           addrsToStrings(p.TailscaleIPs),
		Online:        p.Online,
		Active:        p.Active,
		Expired:       p.Expired,
		ShareeNode:    p.ShareeNode,
		Created:       p.Created,
		LastSeen:      p.LastSeen,
		LastHandshake: p.LastHandshake,
		KeyExpiry:     p.KeyExpiry,
	}
	if p.Tags != nil {
		out.Tags = p.Tags.AsSlice()
	}
	return out
}

func addrsToStrings(addrs []netip.Addr) []string {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.IsValid() {
			out = append(out, a.String())
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
