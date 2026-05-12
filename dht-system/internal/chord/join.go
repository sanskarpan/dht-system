package chord

import (
	"context"
	"fmt"
	"math/big"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// keySpaceSize = 2^160
var keySpaceSize = new(big.Int).Exp(big.NewInt(2), big.NewInt(160), nil)

// CreateRing initialises this node as the sole member of a new Chord ring.
// All fingers point to self; predecessor is nil.
func (n *ChordNode) CreateRing() {
	self := n.selfRef()
	n.mu.Lock()
	n.successor = &self
	n.predecessor = nil
	for i := 0; i < fingerBits; i++ {
		n.fingers[i] = &self
	}
	n.mu.Unlock()

	n.Bus.Publish(events.MakeEvent(events.EventNodeJoin, events.NodeJoinPayload{
		NodeID: consistent.IDToHex(n.ID),
		Addr:   n.Addr,
	}))
}

// Join causes this node to join an existing ring via a known bootstrap node.
// Follows the Chord paper join sequence:
//  1. FindSuccessor to locate our successor.
//  2. InitFingerTable via the bootstrap node.
//  3. UpdateOthers so existing nodes can update their finger tables.
//  4. MigrateKeys — take ownership of keys in (predecessor, n].
func (n *ChordNode) Join(ctx context.Context, bootstrap transport.NodeRef) error {
	ctx = transport.WithSender(ctx, n.selfRef())
	// Step 1 — find our immediate successor.
	succ, err := n.Transport.FindSuccessor(ctx, bootstrap, n.ID)
	if err != nil {
		return fmt.Errorf("chord.Join find successor: %w", err)
	}

	// Step 2 — update local state.
	n.mu.Lock()
	n.successor = &succ
	n.fingers[0] = &succ
	n.predecessor = nil
	n.mu.Unlock()

	// Step 3 — fill the finger table using the bootstrap node.
	if err := n.initFingerTable(ctx, bootstrap); err != nil {
		// Non-fatal: periodic fix_fingers will repair over time.
		n.Logger.Sugar().Warnf("initFingerTable: %v", err)
	}

	// Step 3b — build the anti-finger table (backward lookup shortcuts).
	if err := n.initAntiFingerTable(ctx, bootstrap); err != nil {
		// Non-fatal: fixAntiFingers will repair over time.
		n.Logger.Sugar().Warnf("initAntiFingerTable: %v", err)
	}

	// Step 4 — tell other nodes to update their finger tables.
	n.updateOthers(ctx)

	// Step 5 — migrate keys from our successor that now belong to us.
	if err := n.migrateKeys(ctx); err != nil {
		n.Logger.Sugar().Warnf("migrateKeys: %v", err)
	}

	// Emit join event.
	n.mu.RLock()
	succID := ""
	if n.successor != nil {
		succID = consistent.IDToHex(n.successor.ID)
	}
	n.mu.RUnlock()

	n.Bus.Publish(events.MakeEvent(events.EventNodeJoin, events.NodeJoinPayload{
		NodeID:    consistent.IDToHex(n.ID),
		Addr:      n.Addr,
		Successor: succID,
	}))

	return nil
}

// updateOthers notifies all nodes whose finger tables should now include this node.
// For each i, the node whose i-th finger should point to us is the successor of
// (n.ID - 2^i) mod 2^160.  We call UpdateFingerTable on that node.
func (n *ChordNode) updateOthers(ctx context.Context) {
	nBig := consistent.IDToBigInt(n.ID)

	for i := 0; i < fingerBits; i++ {
		// pos = (n - 2^i) mod 2^160
		pow := consistent.PowerOfTwo(i)
		pos := new(big.Int).Sub(nBig, pow)
		pos.Mod(pos, keySpaceSize)
		if pos.Sign() < 0 {
			pos.Add(pos, keySpaceSize)
		}
		posID := consistent.BigIntToID(pos)

		// Find the successor of pos — that node needs to update its finger[i].
		p, _, err := n.FindSuccessor(ctx, posID)
		if err != nil || p.ID == n.ID {
			continue
		}
		if err := n.Transport.UpdateFingerTable(ctx, p, n.selfRef(), i); err != nil {
			n.Logger.Sugar().Debugf("updateOthers UpdateFingerTable[%d] to %s: %v", i, p.Addr, err)
		}
	}
}

// migrateKeys requests our successor to hand over keys in (predecessor, n].
// Keys are fetched from the successor and stored locally.
func (n *ChordNode) migrateKeys(ctx context.Context) error {
	n.mu.RLock()
	succ := n.successor
	pred := n.predecessor
	n.mu.RUnlock()

	if succ == nil || succ.ID == n.ID {
		// Only node in the ring — nothing to migrate.
		return nil
	}

	if pred == nil {
		// Predecessor not yet known; stabilization will re-trigger migration
		// once Notify establishes the predecessor.
		return nil
	}

	// Ask the successor to push keys in (pred.ID, n.ID] to us via TransferKeys.
	// A nil entries slice is the signal that the caller wants an inbound transfer.
	if err := n.Transport.TransferKeys(ctx, *succ, nil); err != nil {
		n.Logger.Sugar().Debugf("migrateKeys TransferKeys from %s: %v", succ.Addr, err)
	}

	// Pull entries directly from the successor's store for the range we own.
	// In the in-process transport the successor's HandleTransferKeys stores entries
	// into this node via the registry; for a real transport a dedicated RPC would
	// be used.  The call above is sufficient for correctness in the in-process case;
	// stabilization covers any gaps.
	entries := pullKeysInRange(ctx, n, *succ, pred.ID, n.ID)
	if len(entries) == 0 {
		return nil
	}

	for _, e := range entries {
		n.Store.Put(e)
	}

	n.Bus.Publish(events.MakeEvent(events.EventKeyMigrate, events.KeyMigratePayload{
		FromNode: consistent.IDToHex(succ.ID),
		ToNode:   consistent.IDToHex(n.ID),
		KeyCount: len(entries),
	}))

	return nil
}

// pullKeysInRange retrieves entries in (lo, hi] from the target node.
// It fetches all entries from the target and filters to the specified range.
// This is used during join to take ownership of keys from our successor.
func pullKeysInRange(
	ctx context.Context,
	n *ChordNode,
	from transport.NodeRef,
	lo, hi [20]byte,
) []*store.ValueEntry {
	all, err := n.Transport.GetAllEntries(ctx, from)
	if err != nil {
		n.Logger.Sugar().Warnf("pullKeysInRange GetAllEntries from %s: %v", from.Addr, err)
		return nil
	}
	var result []*store.ValueEntry
	for _, e := range all {
		if consistent.IsInIntervalRightClosed(e.Key, lo, hi) {
			result = append(result, e)
		}
	}
	return result
}
