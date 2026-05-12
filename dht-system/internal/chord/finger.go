package chord

import (
	"context"
	"fmt"
	"math/big"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// initFingerTable initializes the finger table using a bootstrap node.
// Per Chord paper: each finger[i] = find_successor(n + 2^i).
// Optimization: reuse finger[i-1] when (n + 2^i) falls in [n, finger[i-1]).
func (n *ChordNode) initFingerTable(ctx context.Context, bootstrap transport.NodeRef) error {
	ctx = transport.WithSender(ctx, n.selfRef())
	// finger[0] — our immediate successor.
	start0 := n.fingerStart(0)
	succ, err := n.Transport.FindSuccessor(ctx, bootstrap, start0)
	if err != nil {
		return fmt.Errorf("chord.initFingerTable finger[0]: %w", err)
	}

	n.mu.Lock()
	n.fingers[0] = &succ
	n.successor = &succ
	n.mu.Unlock()

	// fingers[1..159]
	for i := 1; i < fingerBits; i++ {
		start := n.fingerStart(i)

		n.mu.RLock()
		prevFinger := n.fingers[i-1]
		n.mu.RUnlock()

		// If start ∈ [n, finger[i-1]], we can reuse finger[i-1] without an RPC.
		if prevFinger != nil && consistent.IsInIntervalRightClosed(start, n.ID, prevFinger.ID) {
			n.mu.Lock()
			n.fingers[i] = prevFinger
			n.mu.Unlock()
		} else {
			f, err := n.Transport.FindSuccessor(ctx, bootstrap, start)
			if err != nil {
				// Non-fatal: stabilization will repair over time.
				n.Logger.Sugar().Warnf("initFingerTable finger[%d]: %v", i, err)
				continue
			}
			n.mu.Lock()
			n.fingers[i] = &f
			n.mu.Unlock()
		}
	}
	return nil
}

// fixFingersAtIndex refreshes finger[i] by performing a local FindSuccessor.
func (n *ChordNode) fixFingersAtIndex(ctx context.Context, i int) {
	if i < 0 || i >= fingerBits {
		return
	}
	start := n.fingerStart(i)
	succ, _, err := n.FindSuccessor(ctx, start)
	if err != nil {
		return
	}

	n.mu.Lock()
	n.fingers[i] = &succ
	n.mu.Unlock()
}

// UpdateFingerTable updates finger[i] if s is a better (closer) entry than the current one.
// Called by remote nodes during their join sequence (update_others).
func (n *ChordNode) UpdateFingerTable(ctx context.Context, s transport.NodeRef, i int) error {
	ctx = transport.WithSender(ctx, n.selfRef())
	if i < 0 || i >= fingerBits {
		return fmt.Errorf("chord.UpdateFingerTable: index %d out of range", i)
	}

	n.mu.Lock()
	current := n.fingers[i]

	// Update if: finger[i] is nil, or s ∈ [fingerStart(i), finger[i]).
	// The condition from the Chord paper: s ∈ [n + 2^i, finger[i].id)
	shouldUpdate := current == nil || consistent.IsInInterval(s.ID, n.ID, current.ID)
	if shouldUpdate {
		n.fingers[i] = &s
		if i == 0 {
			n.successor = &s
		}
		pred := n.predecessor
		n.mu.Unlock()

		// Propagate to our predecessor so it can also update its table.
		if pred != nil && pred.ID != n.ID && pred.ID != s.ID {
			_ = n.Transport.UpdateFingerTable(ctx, *pred, s, i)
		}
	} else {
		n.mu.Unlock()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Anti-finger table
// ---------------------------------------------------------------------------

// antiFingerStart returns (n.ID - 2^i) mod 2^160.
//
// This is the mirror of the standard finger start (n.ID + 2^i): instead of
// probing points *ahead* of n on the ring, the anti-finger probes points
// *behind* n. The subtraction is done in the 160-bit unsigned integer space,
// wrapping around zero via the explicit Sign() < 0 correction below.
//
// Example for a 4-bit ring (IDs 0–15, n.ID = 3):
//
//	i=0 → start = (3 - 1) mod 16 = 2   (one step back)
//	i=1 → start = (3 - 2) mod 16 = 1   (two steps back)
//	i=3 → start = (3 - 8) mod 16 = 11  (wraps around)
func (n *ChordNode) antiFingerStart(i int) [20]byte {
	nBig := consistent.IDToBigInt(n.ID)
	pow := consistent.PowerOfTwo(i)
	result := new(big.Int).Sub(nBig, pow)
	// big.Int.Mod can return a negative result when the dividend is negative;
	// manually correct by adding the modulus.
	result.Mod(result, keySpaceSize)
	if result.Sign() < 0 {
		result.Add(result, keySpaceSize)
	}
	return consistent.BigIntToID(result)
}

// initAntiFingerTable builds the anti-finger table using a bootstrap node.
// For each i, pos = (n.ID - 2^i) mod 2^160.
// We find the successor of pos via RPC, then ask that node for its predecessor.
// The predecessor is the node whose range includes pos, so it is the correct
// antiFinger[i] entry.  Errors on individual entries are non-fatal (warn and
// continue); stabilization will repair the table over time.
func (n *ChordNode) initAntiFingerTable(ctx context.Context, bootstrap transport.NodeRef) error {
	ctx = transport.WithSender(ctx, n.selfRef())
	for i := 0; i < fingerBits; i++ {
		pos := n.antiFingerStart(i)

		// Find the successor of pos via the bootstrap node.
		succ, err := n.Transport.FindSuccessor(ctx, bootstrap, pos)
		if err != nil {
			n.Logger.Sugar().Warnf("initAntiFingerTable[%d]: FindSuccessor: %v", i, err)
			continue
		}

		// The predecessor of that successor is the node that owns pos.
		pred, err := n.Transport.GetPredecessor(ctx, succ)
		if err != nil || pred == nil {
			// Fall back: the successor itself is a reasonable approximation.
			n.mu.Lock()
			n.antiFingers[i] = &succ
			n.mu.Unlock()
			if err != nil {
				n.Logger.Sugar().Warnf("initAntiFingerTable[%d]: GetPredecessor: %v; using successor as fallback", i, err)
			}
			continue
		}

		n.mu.Lock()
		n.antiFingers[i] = pred
		n.mu.Unlock()
	}
	return nil
}

// fixAntiFingers refreshes one anti-finger entry per call, rotating through
// all 160 indices.  The rotating index is stored as antiFingerIdx on the node.
func (n *ChordNode) fixAntiFingers(ctx context.Context) {
	ctx = transport.WithSender(ctx, n.selfRef())
	n.mu.Lock()
	idx := n.antiFingerIdx
	n.antiFingerIdx = (n.antiFingerIdx + 1) % fingerBits
	n.mu.Unlock()

	pos := n.antiFingerStart(idx)

	// Use local FindSuccessor (cheaper than a bootstrap RPC once the ring is stable).
	succ, _, err := n.FindSuccessor(ctx, pos)
	if err != nil {
		return
	}

	pred, err := n.Transport.GetPredecessor(ctx, succ)
	if err != nil || pred == nil {
		// Fall back to successor.
		n.mu.Lock()
		n.antiFingers[idx] = &succ
		n.mu.Unlock()
		return
	}

	n.mu.Lock()
	n.antiFingers[idx] = pred
	n.mu.Unlock()
}

// GetAntiFinger returns the anti-finger at index i (thread-safe).
// Returns nil if i is out of range or the entry has not been initialised yet.
func (n *ChordNode) GetAntiFinger(i int) *transport.NodeRef {
	if i < 0 || i >= fingerBits {
		return nil
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.antiFingers[i]
}
