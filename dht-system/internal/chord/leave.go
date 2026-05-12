package chord

import (
	"context"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// Leave gracefully removes this node from the ring:
//  1. Transfer all owned keys to the successor.
//  2. Update the successor's predecessor pointer to our predecessor.
//  3. Update the predecessor's successor pointer to our successor.
//  4. Stop all background goroutines.
func (n *ChordNode) Leave(ctx context.Context) error {
	ctx = transport.WithSender(ctx, n.selfRef())
	n.mu.RLock()
	succ := n.successor
	pred := n.predecessor
	n.mu.RUnlock()

	var transferred int

	if succ != nil && succ.ID != n.ID {
		// Dump all local keys to the successor.
		entries := n.Store.All()
		if len(entries) > 0 {
			if err := n.Transport.TransferKeys(ctx, *succ, entries); err != nil {
				n.Logger.Sugar().Warnf("Leave: TransferKeys to successor failed: %v", err)
			} else {
				transferred = len(entries)
			}
		}

		// Tell the successor that its new predecessor is our predecessor.
		// We reuse the Notify RPC: the successor's HandleNotify will update
		// its predecessor if the candidate is in the right interval.
		if pred != nil {
			_ = n.Transport.Notify(ctx, *succ, *pred)
		}
	}

	// Tell our predecessor that its new successor is our successor.
	// We reuse Notify in reverse: ask predecessor to treat our successor
	// as the node that notified it (the predecessor updates nothing via Notify,
	// but stabilize on the predecessor will pick up the new topology quickly).
	// A cleaner approach is a dedicated SetSuccessor RPC; absent that, we rely
	// on the stabilization protocol to converge within a few rounds.
	if pred != nil && succ != nil && pred.ID != n.ID {
		// Trigger an immediate stabilize on predecessor by notifying it of succ.
		_ = n.Transport.Notify(ctx, *pred, *succ)
	}

	n.Bus.Publish(events.MakeEvent(events.EventNodeLeave, events.NodeLeavePayload{
		NodeID:          consistent.IDToHex(n.ID),
		KeysTransferred: transferred,
	}))

	n.Stop()
	return nil
}

// SimulateCrash abruptly stops the node without any cleanup, simulating a
// sudden hardware or network failure.
func (n *ChordNode) SimulateCrash() {
	n.Bus.Publish(events.MakeEvent(events.EventNodeCrash, events.NodeCrashPayload{
		NodeID: consistent.IDToHex(n.ID),
	}))
	n.Stop()
}
