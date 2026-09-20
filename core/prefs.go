package core

import (
	"context"
	"net/netip"
	"strconv"
	"strings"

	"tailscale.com/ipn"
	"tailscale.com/tailcfg"
)

// SetExitNode routes this node's outbound traffic to non-tailnet addresses
// through the given exit node. Dials to tailnet addresses are unaffected.
//
// nodeID is the stable node ID of an exit node, as reported by Status.Peers
// (Peer.ID).
func (n *Node) SetExitNode(ctx context.Context, nodeID string) error {
	lc, err := n.LocalClient()
	if err != nil {
		return err
	}
	if strings.TrimSpace(nodeID) == "" {
		return newError(ErrCodeInvalidArgument, "exit-node", "node ID must not be empty", nil)
	}
	_, err = lc.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs: ipn.Prefs{
			ExitNodeID: tailcfg.StableNodeID(nodeID),
			RouteAll:   true,
		},
		ExitNodeIDSet: true,
		RouteAllSet:   true,
	})
	if err != nil {
		return newError(ErrCodeBackend, "exit-node", "unable to use exit node "+nodeID, err)
	}
	return nil
}

// ClearExitNode stops using the previously selected exit node.
func (n *Node) ClearExitNode(ctx context.Context) error {
	lc, err := n.LocalClient()
	if err != nil {
		return err
	}
	if err := lc.SetUseExitNode(ctx, false); err != nil {
		return newError(ErrCodeBackend, "exit-node", "unable to clear the exit node", err)
	}
	return nil
}

// Ping measures reachability of a tailnet IP without involving the OS IP
// stack. It is the SDK's connectivity diagnostic, equivalent to
// "tailscale ping".
func (n *Node) Ping(ctx context.Context, addr string) (*PingResult, error) {
	lc, err := n.LocalClient()
	if err != nil {
		return nil, err
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(addr))
	if err != nil {
		return nil, newError(ErrCodeInvalidArgument, "ping", "invalid IP address "+addr, err)
	}
	res, err := lc.Ping(ctx, ip, tailcfg.PingTSMP)
	if err != nil {
		return nil, newError(ErrCodeBackend, "ping", "ping to "+addr+" failed", err)
	}
	if res == nil {
		return nil, newError(ErrCodeBackend, "ping", "empty ping result", nil)
	}
	return &PingResult{
		IP:             res.IP,
		NodeIP:         res.NodeIP,
		NodeName:       res.NodeName,
		LatencySeconds: res.LatencySeconds,
		Endpoint:       res.Endpoint,
		DERPRegionID:   res.DERPRegionID,
		DERPRegionCode: res.DERPRegionCode,
		Err:            res.Err,
	}, nil
}

// WhoIs resolves the owner of an incoming tailnet connection. remoteAddr is
// the value of net.Conn.RemoteAddr() on a connection accepted from a tailnet
// listener, i.e. "ip:port".
func (n *Node) WhoIs(ctx context.Context, remoteAddr string) (*Identity, error) {
	lc, err := n.LocalClient()
	if err != nil {
		return nil, err
	}
	if remoteAddr == "" {
		return nil, newError(ErrCodeInvalidArgument, "whois", "remote address must not be empty", nil)
	}
	who, err := lc.WhoIs(ctx, remoteAddr)
	if err != nil {
		return nil, newError(ErrCodeBackend, "whois", "unable to resolve "+remoteAddr, err)
	}
	out := &Identity{}
	if who.UserProfile != nil {
		out.LoginName = who.UserProfile.LoginName
		out.DisplayName = who.UserProfile.DisplayName
		out.ProfilePicURL = who.UserProfile.ProfilePicURL
		out.UserID = strconv.FormatInt(int64(who.UserProfile.ID), 10)
	}
	if who.Node != nil {
		out.NodeID = string(who.Node.StableID)
		out.NodeName = strings.TrimSuffix(who.Node.Name, ".")
		out.Tags = who.Node.Tags
	}
	return out, nil
}
