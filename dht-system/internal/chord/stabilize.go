package chord

import (
	"context"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// Stabilize verifies the successor and notifies it of our existence.
// This is the core stabilization step from the Chord paper:
//  1. Ask successor for its predecessor x.
//  2. If x ∈ (n, successor), adopt x as our new successor.
//  3. Notify our (possibly new) successor that we exist.
//  4. Refresh the successor list.
func (n *ChordNode) Stabilize(ctx context.Context) error {
	ctx = transport.WithSender(ctx, n.selfRef())
	n.mu.RLock()
	succ := n.successor
	n.mu.RUnlock()

	if succ == nil || succ.ID == n.ID {
		// Single-node ring or no successor yet — point back to self.
		self := n.selfRef()
		n.mu.Lock()
		n.successor = &self
		n.fingers[0] = &self
		n.mu.Unlock()
		return nil
	}

	// Ask our successor for its predecessor.
	x, err := n.Transport.GetPredecessor(ctx, *succ)
	if err == nil && x != nil {
		// If x ∈ (n, succ), x is a better successor for us.
		if consistent.IsInInterval(x.ID, n.ID, succ.ID) {
			n.mu.Lock()
			n.successor = x
			n.fingers[0] = x
			succ = x
			n.mu.Unlock()
		}
	}

	// Notify our (possibly updated) successor that we believe we are its predecessor.
	_ = n.Transport.Notify(ctx, *succ, n.selfRef())

	// Keep the successor list fresh.
	n.updateSuccessorList(ctx, *succ)

	// Emit stabilize event.
	n.mu.RLock()
	succID := ""
	predID := ""
	if n.successor != nil {
		succID = consistent.IDToHex(n.successor.ID)
	}
	if n.predecessor != nil {
		predID = consistent.IDToHex(n.predecessor.ID)
	}
	n.mu.RUnlock()

	n.Bus.Publish(events.MakeEvent(events.EventStabilize, events.StabilizePayload{
		NodeID:      consistent.IDToHex(n.ID),
		Successor:   succID,
		Predecessor: predID,
	}))

	return nil
}

// Notify is called by a node n' that believes it should be our predecessor.
// We update our predecessor if n' ∈ (predecessor, n).
func (n *ChordNode) Notify(ctx context.Context, candidate transport.NodeRef) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.predecessor == nil || consistent.IsInInterval(candidate.ID, n.predecessor.ID, n.ID) {
		n.predecessor = &candidate
		n.Bus.Publish(events.MakeEvent(events.EventNotify, events.NotifyPayload{
			NodeID:  consistent.IDToHex(n.ID),
			NewPred: consistent.IDToHex(candidate.ID),
		}))
	}
}

// CheckPredecessor pings our predecessor and clears it if it is unreachable.
func (n *ChordNode) CheckPredecessor(ctx context.Context) {
	ctx = transport.WithSender(ctx, n.selfRef())
	n.mu.RLock()
	pred := n.predecessor
	n.mu.RUnlock()

	if pred == nil {
		return
	}
	ctx2, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := n.Transport.Ping(ctx2, *pred); err != nil {
		n.mu.Lock()
		n.predecessor = nil
		n.mu.Unlock()
		n.Logger.Sugar().Infof("predecessor %s unreachable, cleared", pred.Addr)
	}
}

// CheckSuccessor pings our successor and walks the successor list if it is unreachable.
func (n *ChordNode) CheckSuccessor(ctx context.Context) {
	ctx = transport.WithSender(ctx, n.selfRef())
	n.mu.RLock()
	succ := n.successor
	succList := make([]*transport.NodeRef, len(n.succList))
	copy(succList, n.succList)
	n.mu.RUnlock()

	if succ == nil {
		return
	}
	if succ.ID == n.ID {
		return // single-node ring — nothing to check
	}

	ctx2, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := n.Transport.Ping(ctx2, *succ); err == nil {
		return // successor is alive
	}

	// Successor appears dead — walk the successor list for a live replacement.
	for _, s := range succList {
		if s == nil || s.ID == succ.ID {
			continue
		}
		ctx3, cancel3 := context.WithTimeout(ctx, 300*time.Millisecond)
		pingErr := n.Transport.Ping(ctx3, *s)
		cancel3()
		if pingErr == nil {
			n.mu.Lock()
			n.successor = s
			n.fingers[0] = s
			n.mu.Unlock()
			n.Logger.Sugar().Infof("successor replaced with %s from succList", s.Addr)
			// Immediately run stabilize so the ring heals faster.
			_ = n.Stabilize(ctx)
			return
		}
	}
	n.Logger.Sugar().Warn("all successors in list are unreachable")
}

// updateSuccessorList rebuilds our successor list from the given node.
// The list is: [succ, succ.succList[0], succ.succList[1], ...] up to SuccessorListSize.
func (n *ChordNode) updateSuccessorList(ctx context.Context, succ transport.NodeRef) {
	remoteList, err := n.Transport.GetSuccessorList(ctx, succ)
	if err != nil {
		// Non-fatal: keep the old list.
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	maxLen := n.Config.SuccessorListSize
	combined := make([]*transport.NodeRef, 0, maxLen)
	s := succ
	combined = append(combined, &s)
	for i := range remoteList {
		if len(combined) >= maxLen {
			break
		}
		entry := remoteList[i] // copy to get a stable pointer
		combined = append(combined, &entry)
	}
	n.succList = combined
}

// Background goroutine runners.

// runStabilize drives both the stabilize and fix-fingers loops from a single goroutine
// to avoid the overhead of two separate tickers that could step on each other.
func (n *ChordNode) runStabilize() {
	stabilizeTicker := time.NewTicker(n.Config.StabilizeInterval)
	defer stabilizeTicker.Stop()

	fingerTicker := time.NewTicker(n.Config.FixFingersInterval)
	defer fingerTicker.Stop()

	fingerIdx := 1 // we cycle through indices 1..159; index 0 is maintained by stabilize

	for {
		select {
		case <-n.stopCh:
			return
		case <-stabilizeTicker.C:
			_ = n.Stabilize(n.ctx)
		case <-fingerTicker.C:
			n.fixFingersAtIndex(n.ctx, fingerIdx)
			n.fixAntiFingers(n.ctx)
			fingerIdx++
			if fingerIdx >= fingerBits {
				fingerIdx = 1
			}
		}
	}
}

func (n *ChordNode) runCheckPredecessor() {
	ticker := time.NewTicker(n.Config.CheckPredInterval)
	defer ticker.Stop()
	for {
		select {
		case <-n.stopCh:
			return
		case <-ticker.C:
			n.CheckPredecessor(n.ctx)
		}
	}
}

func (n *ChordNode) runCheckSuccessor() {
	ticker := time.NewTicker(n.Config.CheckSuccInterval)
	defer ticker.Stop()
	for {
		select {
		case <-n.stopCh:
			return
		case <-ticker.C:
			n.CheckSuccessor(n.ctx)
		}
	}
}
