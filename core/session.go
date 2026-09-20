package core

import (
	"context"

	"tailscale.com/ipn"
)

// Logout deauthorizes this node and clears its session state, so the next
// start requires a fresh interactive login.
//
// Note: the device entry stays visible in the tailnet admin console until an
// administrator deletes it.
func (n *Node) Logout(ctx context.Context) error {
	lc, err := n.LocalClient()
	if err != nil {
		return err
	}
	if err := lc.Logout(ctx); err != nil {
		return newError(ErrCodeBackend, "logout", "unable to log out", err)
	}
	return nil
}

// ProfileStatus returns the active login profile plus every stored profile.
func (n *Node) ProfileStatus(ctx context.Context) (*Profile, []Profile, error) {
	lc, err := n.LocalClient()
	if err != nil {
		return nil, nil, err
	}
	current, all, err := lc.ProfileStatus(ctx)
	if err != nil {
		return nil, nil, newError(ErrCodeBackend, "profiles", "unable to read login profiles", err)
	}
	cur := mapProfile(current)
	out := make([]Profile, 0, len(all))
	for _, p := range all {
		out = append(out, mapProfile(p))
	}
	return &cur, out, nil
}

// SwitchProfile activates a previously stored login profile by its ID.
func (n *Node) SwitchProfile(ctx context.Context, id string) error {
	lc, err := n.LocalClient()
	if err != nil {
		return err
	}
	if id == "" {
		return newError(ErrCodeInvalidArgument, "switch-profile", "profile ID must not be empty", nil)
	}
	if err := lc.SwitchProfile(ctx, ipn.ProfileID(id)); err != nil {
		return newError(ErrCodeBackend, "switch-profile", "unable to switch to profile "+id, err)
	}
	return nil
}

// DeleteProfile removes a stored login profile by its ID.
func (n *Node) DeleteProfile(ctx context.Context, id string) error {
	lc, err := n.LocalClient()
	if err != nil {
		return err
	}
	if id == "" {
		return newError(ErrCodeInvalidArgument, "delete-profile", "profile ID must not be empty", nil)
	}
	if err := lc.DeleteProfile(ctx, ipn.ProfileID(id)); err != nil {
		return newError(ErrCodeBackend, "delete-profile", "unable to delete profile "+id, err)
	}
	return nil
}

func mapProfile(p ipn.LoginProfile) Profile {
	return Profile{
		ID:          string(p.ID),
		Name:        p.Name,
		LoginName:   p.UserProfile.LoginName,
		DisplayName: p.UserProfile.DisplayName,
		NodeID:      string(p.NodeID),
		ControlURL:  p.ControlURL,
	}
}
