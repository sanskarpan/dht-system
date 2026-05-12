package kademlia

import (
	"context"
	"fmt"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// Ensure KademliaNode implements NodeHandler.
var _ transport.NodeHandler = (*KademliaNode)(nil)

// HandleKPing handles a PING RPC. Updates routing table with sender.
func (n *KademliaNode) HandleKPing(ctx context.Context, sender transport.NodeRef) error {
	if sender.Addr != "" {
		n.updateRoutingTable(Contact{ID: NodeID(sender.ID), Addr: sender.Addr, LastSeen: time.Now()})
	}
	return nil
}

// HandleKStore stores a key-value entry locally. Updates routing table with sender.
func (n *KademliaNode) HandleKStore(ctx context.Context, sender transport.NodeRef, entry *store.ValueEntry) error {
	if sender.Addr != "" {
		n.updateRoutingTable(Contact{ID: NodeID(sender.ID), Addr: sender.Addr, LastSeen: time.Now()})
	}
	n.KVStore.Put(entry)
	if n.antiEntropy != nil {
		n.antiEntropy.NotifyPut(entry.Key, entry.Value)
	}
	return nil
}

// HandleFindNode returns the k closest contacts to the target ID.
func (n *KademliaNode) HandleFindNode(ctx context.Context, sender transport.NodeRef, id [20]byte) ([]transport.NodeRef, error) {
	if sender.Addr != "" {
		n.updateRoutingTable(Contact{ID: NodeID(sender.ID), Addr: sender.Addr, LastSeen: time.Now()})
	}
	target := NodeID(id)
	contacts := n.RoutingTable.KClosest(target, n.Config.K)
	refs := make([]transport.NodeRef, len(contacts))
	for i, c := range contacts {
		refs[i] = transport.NodeRef{ID: [20]byte(c.ID), Addr: c.Addr}
	}
	return refs, nil
}

// HandleFindValue checks local store; if found returns value, else returns k closest contacts.
func (n *KademliaNode) HandleFindValue(ctx context.Context, sender transport.NodeRef, key [20]byte) (*transport.FindValueResult, error) {
	if sender.Addr != "" {
		n.updateRoutingTable(Contact{ID: NodeID(sender.ID), Addr: sender.Addr, LastSeen: time.Now()})
	}

	entry, ok := n.KVStore.Get(key)
	if ok {
		return &transport.FindValueResult{Found: true, Entry: entry}, nil
	}

	// Not found: return k closest contacts
	contacts := n.RoutingTable.KClosest(NodeID(key), n.Config.K)
	refs := make([]transport.NodeRef, len(contacts))
	for i, c := range contacts {
		refs[i] = transport.NodeRef{ID: [20]byte(c.ID), Addr: c.Addr}
	}
	return &transport.FindValueResult{Found: false, Contacts: refs}, nil
}

// HandlePing responds to a common liveness check.
func (n *KademliaNode) HandlePing(ctx context.Context) error {
	return nil
}

// HandleGetEntry retrieves a value from the local store.
func (n *KademliaNode) HandleGetEntry(ctx context.Context, key [20]byte) (*store.ValueEntry, error) {
	entry, ok := n.KVStore.Get(key)
	if !ok {
		return nil, fmt.Errorf("key %s not found", consistent.IDToHex(key))
	}
	return entry, nil
}

// HandlePutEntry stores a value in the local store.
func (n *KademliaNode) HandlePutEntry(ctx context.Context, key [20]byte, entry *store.ValueEntry) error {
	n.KVStore.Put(entry)
	if n.antiEntropy != nil {
		n.antiEntropy.NotifyPut(entry.Key, entry.Value)
	}
	return nil
}

// HandleGetAllEntries returns all key-value entries in the local store.
func (n *KademliaNode) HandleGetAllEntries(ctx context.Context) ([]*store.ValueEntry, error) {
	return n.KVStore.All(), nil
}

// Chord-specific handlers — not applicable to Kademlia nodes.

func (n *KademliaNode) HandleFindSuccessor(ctx context.Context, id [20]byte) (transport.NodeRef, error) {
	return transport.NodeRef{}, fmt.Errorf("kademlia node does not handle Chord RPCs")
}

func (n *KademliaNode) HandleGetPredecessor(ctx context.Context) (*transport.NodeRef, error) {
	return nil, fmt.Errorf("kademlia node does not handle Chord RPCs")
}

func (n *KademliaNode) HandleNotify(ctx context.Context, sender transport.NodeRef) error {
	return fmt.Errorf("kademlia node does not handle Chord RPCs")
}

func (n *KademliaNode) HandleGetSuccessorList(ctx context.Context) ([]transport.NodeRef, error) {
	return nil, fmt.Errorf("kademlia node does not handle Chord RPCs")
}

func (n *KademliaNode) HandleTransferKeys(ctx context.Context, entries []*store.ValueEntry) error {
	return fmt.Errorf("kademlia node does not handle Chord RPCs")
}

func (n *KademliaNode) HandleUpdateFingerTable(ctx context.Context, s transport.NodeRef, i int) error {
	return fmt.Errorf("kademlia node does not handle Chord RPCs")
}
