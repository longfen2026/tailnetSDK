package core

import (
	"context"
	"time"

	"tailscale.com/ipn"
)

// peersSnapshotInterval rate limits the extra LocalAPI status reads that are
// used to turn netmap notifications into the SDK's own peer list.
const peersSnapshotInterval = time.Second

// Watch subscribes to backend notifications and invokes fn for every event
// until the returned stop function is called or ctx is canceled.
//
// Events are delivered from a dedicated goroutine, so fn must not block: hand
// work off to a queue if the host UI needs it.
func (n *Node) Watch(ctx context.Context, fn func(Event)) (func(), error) {
	if fn == nil {
		return nil, newError(ErrCodeInvalidArgument, "watch", "callback must not be nil", nil)
	}
	lc, err := n.LocalClient()
	if err != nil {
		return nil, err
	}
	wctx, cancel := context.WithCancel(ctx)
	w, err := lc.WatchIPNBus(wctx, ipn.NotifyInitialState|ipn.NotifyInitialHealthState|ipn.NotifyInitialStatus)
	if err != nil {
		cancel()
		return nil, newError(ErrCodeBackend, "watch", "unable to subscribe to the IPN bus", err)
	}

	stop := func() {
		cancel()
		_ = w.Close()
	}
	go func() {
		defer w.Close()
		var lastSnapshot time.Time
		for {
			msg, err := w.Next()
			if err != nil {
				// A canceled context is a normal shutdown, not an error.
				if wctx.Err() == nil {
					fn(Event{Kind: EventError, Time: time.Now(), Message: err.Error()})
				}
				return
			}
			now := time.Now()
			if msg.BrowseToURL != nil {
				fn(Event{Kind: EventAuthURL, Time: now, AuthURL: *msg.BrowseToURL})
			}
			if msg.State != nil {
				fn(Event{Kind: EventState, Time: now, State: stateFromIPN(*msg.State)})
			}
			if msg.LoginFinished != nil {
				fn(Event{Kind: EventLoginFinished, Time: now})
			}
			if msg.Health != nil {
				// Notify carries the upstream health.State; the string form the
				// SDK exposes comes from the LocalAPI status snapshot below.
				fn(Event{Kind: EventHealth, Time: now})
			}

			// Netmap/self/state changes all mean "peers may have changed".
			// Re-reading the LocalAPI status is cheap and gives us the same
			// fields the rest of the SDK exposes.
			snapshot := msg.InitialStatus != nil || msg.SelfChange != nil || msg.NetMap != nil ||
				(msg.State != nil && *msg.State == ipn.Running)
			if snapshot && now.Sub(lastSnapshot) >= peersSnapshotInterval {
				lastSnapshot = now
				if st, err := n.Status(ctx); err == nil {
					fn(Event{Kind: EventSelf, Time: now, Self: st.Self, Health: st.Health})
					fn(Event{Kind: EventPeers, Time: now, PeerCount: len(st.Peers)})
				}
			}
		}
	}()
	return stop, nil
}
