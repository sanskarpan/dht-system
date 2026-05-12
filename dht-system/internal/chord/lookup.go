package chord

import (
	"context"
	"fmt"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// FindSuccessor returns the successor node responsible for id.
// It performs an iterative lookup: walk the finger table locally until
// the responsible node is identified, then delegate to remote nodes as needed.
// Returns the node reference and a trace of hops.
func (n *ChordNode) FindSuccessor(ctx context.Context, id [20]byte) (transport.NodeRef, []transport.HopEvent, error) {
	ctx = transport.WithSender(ctx, n.selfRef())
	var hops []transport.HopEvent
	hopIdx := 0

	// Start at the local node and iteratively route.
	current := n.selfRef()

	for {
		if hopIdx > n.Config.MaxHops {
			return transport.NodeRef{}, hops, fmt.Errorf("chord.FindSuccessor: max hops exceeded (%d)", n.Config.MaxHops)
		}

		// Obtain the successor of the current node.
		var succ transport.NodeRef
		if current.ID == n.ID {
			// Local: read directly from memory.
			n.mu.RLock()
			if n.successor != nil {
				succ = *n.successor
			} else {
				succ = n.selfRef()
			}
			n.mu.RUnlock()
		} else {
			// Remote: issue an RPC to current and return its answer directly.
			// The remote node will continue the lookup recursively on its own.
			hop := transport.HopEvent{
				FromNode:  n.Addr,
				ToNode:    current.Addr,
				Mechanism: "rpc",
				HopIndex:  hopIdx,
			}
			hops = append(hops, hop)
			n.Bus.Publish(events.MakeEvent(events.EventLookupHop, events.LookupHopPayload{
				FromNode:  consistent.IDToHex(n.ID),
				ToNode:    consistent.IDToHex(current.ID),
				Mechanism: "rpc",
				HopIndex:  hopIdx,
			}))

			result, err := n.Transport.FindSuccessor(ctx, current, id)
			if err != nil {
				return transport.NodeRef{}, hops, fmt.Errorf("chord.FindSuccessor RPC to %s: %w", current.Addr, err)
			}
			return result, hops, nil
		}

		// Check if id ∈ (current, succ] — succ is the answer.
		if consistent.IsInIntervalRightClosed(id, current.ID, succ.ID) {
			return succ, hops, nil
		}

		// Find the closest preceding finger to id.
		closest := n.ClosestPrecedingNode(id)

		hop := transport.HopEvent{
			FromNode:  current.Addr,
			ToNode:    closest.Addr,
			Mechanism: "finger",
			HopIndex:  hopIdx,
		}
		hops = append(hops, hop)
		n.Bus.Publish(events.MakeEvent(events.EventLookupHop, events.LookupHopPayload{
			FromNode:  consistent.IDToHex(current.ID),
			ToNode:    consistent.IDToHex(closest.ID),
			Mechanism: "finger",
			HopIndex:  hopIdx,
		}))

		// No progress — closest is still us; return the successor we found.
		if closest.ID == current.ID {
			return succ, hops, nil
		}

		// If the closest preceding node is remote, delegate the remainder of the
		// lookup to it via RPC and return its answer.
		if closest.ID != n.ID {
			hopIdx++
			remoteHop := transport.HopEvent{
				FromNode:  n.Addr,
				ToNode:    closest.Addr,
				Mechanism: "rpc_forward",
				HopIndex:  hopIdx,
			}
			hops = append(hops, remoteHop)
			n.Bus.Publish(events.MakeEvent(events.EventLookupHop, events.LookupHopPayload{
				FromNode:  consistent.IDToHex(n.ID),
				ToNode:    consistent.IDToHex(closest.ID),
				Mechanism: "rpc_forward",
				HopIndex:  hopIdx,
			}))

			result, err := n.Transport.FindSuccessor(ctx, closest, id)
			if err != nil {
				return transport.NodeRef{}, hops, fmt.Errorf("chord.FindSuccessor forward to %s: %w", closest.Addr, err)
			}
			return result, hops, nil
		}

		// closest == n.ID means we keep iterating locally (shouldn't happen normally,
		// but guard against it).
		current = closest
		hopIdx++
	}
}

// ClosestPrecedingNode returns the node (from the forward finger table or the
// anti-finger table) that is closest to id while strictly preceding it on the
// ring (open interval (n, id)).
//
// Primary path: scan the forward finger table (high-to-low index) for the
// best clockwise shortcut toward id — O(log N) forward lookup.
//
// Backward path: if a key sits close behind us (i.e., in the arc
// (predecessor, n.ID) looking backward), the anti-finger table can surface a
// better starting point by pointing to nodes near (n.ID - 2^i).  We merge the
// two candidates and return whichever is closer to id in the clockwise sense.
func (n *ChordNode) ClosestPrecedingNode(id [20]byte) transport.NodeRef {
	n.mu.RLock()
	defer n.mu.RUnlock()

	// -- Forward finger scan (primary path) --
	var best *transport.NodeRef
	for i := fingerBits - 1; i >= 0; i-- {
		f := n.fingers[i]
		if f == nil {
			continue
		}
		if consistent.IsInInterval(f.ID, n.ID, id) {
			best = f
			break
		}
	}

	// -- Anti-finger scan (backward path) --
	// Only consult anti-fingers when the key appears to lie in the arc
	// (predecessor, n.ID) — i.e., the key is "behind" this node and a
	// counter-clockwise shortcut may yield a better hop.
	pred := n.predecessor
	if pred != nil && consistent.IsInInterval(id, pred.ID, n.ID) {
		for i := fingerBits - 1; i >= 0; i-- {
			af := n.antiFingers[i]
			if af == nil {
				continue
			}
			// The anti-finger must itself be in (n, id) to be a valid
			// clockwise predecessor of id.
			if consistent.IsInInterval(af.ID, n.ID, id) {
				// Pick whichever of the two candidates is closer to id
				// (i.e., farther from n in the clockwise direction).
				if best == nil || consistent.IsInInterval(best.ID, n.ID, af.ID) {
					best = af
				}
				break
			}
		}
	}

	if best != nil {
		return *best
	}
	// Fall back to self.
	return n.selfRef()
}
