package chord

import (
	"context"
	"fmt"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// Compile-time assertion: ChordNode must satisfy the NodeHandler interface.
var _ transport.NodeHandler = (*ChordNode)(nil)

// ---------------------------------------------------------------------------
// Chord RPC handlers
// ---------------------------------------------------------------------------

// HandleFindSuccessor handles an incoming FindSuccessor RPC.
func (n *ChordNode) HandleFindSuccessor(ctx context.Context, id [20]byte) (transport.NodeRef, error) {
	result, _, err := n.FindSuccessor(ctx, id)
	return result, err
}

// HandleGetPredecessor returns our current predecessor.
func (n *ChordNode) HandleGetPredecessor(ctx context.Context) (*transport.NodeRef, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.predecessor, nil
}

// HandleNotify processes an incoming Notify RPC.
// The sender claims to be (or to know of) our new predecessor.
func (n *ChordNode) HandleNotify(ctx context.Context, sender transport.NodeRef) error {
	n.Notify(ctx, sender)
	return nil
}

// HandleGetSuccessorList returns our current successor list.
func (n *ChordNode) HandleGetSuccessorList(ctx context.Context) ([]transport.NodeRef, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	result := make([]transport.NodeRef, 0, len(n.succList))
	for _, s := range n.succList {
		if s != nil {
			result = append(result, *s)
		}
	}
	return result, nil
}

// HandleTransferKeys stores incoming key-value entries into the local store.
// A nil entries slice is a no-op (used as a signal in some migration paths).
func (n *ChordNode) HandleTransferKeys(ctx context.Context, entries []*store.ValueEntry) error {
	if entries == nil {
		return nil
	}
	for _, e := range entries {
		if e != nil {
			n.Store.Put(e)
		}
	}
	return nil
}

// HandleUpdateFingerTable handles an incoming UpdateFingerTable RPC.
func (n *ChordNode) HandleUpdateFingerTable(ctx context.Context, s transport.NodeRef, i int) error {
	return n.UpdateFingerTable(ctx, s, i)
}

// ---------------------------------------------------------------------------
// Common RPC handlers
// ---------------------------------------------------------------------------

// HandlePing responds to a liveness check.
func (n *ChordNode) HandlePing(ctx context.Context) error {
	return nil
}

// HandleGetEntry retrieves a single value from the local store by key.
func (n *ChordNode) HandleGetEntry(ctx context.Context, key [20]byte) (*store.ValueEntry, error) {
	entry, ok := n.Store.Get(key)
	if !ok {
		return nil, fmt.Errorf("chord.HandleGetEntry: key %s not found", consistent.IDToHex(key))
	}
	return entry, nil
}

// HandlePutEntry stores a single key-value entry into the local store.
func (n *ChordNode) HandlePutEntry(ctx context.Context, key [20]byte, entry *store.ValueEntry) error {
	if entry == nil {
		return fmt.Errorf("chord.HandlePutEntry: nil entry for key %s", consistent.IDToHex(key))
	}
	n.Store.Put(entry)
	return nil
}

// HandleGetAllEntries returns all key-value entries in the local store.
func (n *ChordNode) HandleGetAllEntries(ctx context.Context) ([]*store.ValueEntry, error) {
	return n.Store.All(), nil
}

// ---------------------------------------------------------------------------
// Kademlia RPC handlers — not applicable to a Chord node.
// ---------------------------------------------------------------------------

// HandleKPing is not used by Chord nodes.
func (n *ChordNode) HandleKPing(ctx context.Context, sender transport.NodeRef) error {
	return fmt.Errorf("chord node does not handle Kademlia RPCs (KPing)")
}

// HandleKStore is not used by Chord nodes.
func (n *ChordNode) HandleKStore(ctx context.Context, sender transport.NodeRef, entry *store.ValueEntry) error {
	return fmt.Errorf("chord node does not handle Kademlia RPCs (KStore)")
}

// HandleFindNode is not used by Chord nodes.
func (n *ChordNode) HandleFindNode(ctx context.Context, sender transport.NodeRef, id [20]byte) ([]transport.NodeRef, error) {
	return nil, fmt.Errorf("chord node does not handle Kademlia RPCs (FindNode)")
}

// HandleFindValue is not used by Chord nodes.
func (n *ChordNode) HandleFindValue(ctx context.Context, sender transport.NodeRef, key [20]byte) (*transport.FindValueResult, error) {
	return nil, fmt.Errorf("chord node does not handle Kademlia RPCs (FindValue)")
}
