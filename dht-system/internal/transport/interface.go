// Package transport defines the RPC abstraction layer for DHT node communication.
//
// The Transport interface captures all inter-node RPCs for both Chord and Kademlia.
// The primary implementation is InProcessTransport, which dispatches calls via a
// thread-safe in-memory registry (suitable for simulation).  FaultInjectingTransport
// wraps any Transport to add configurable latency and packet-loss simulation.
//
// NodeHandler is the complementary interface: each DHT node must implement it to
// receive incoming RPCs from the transport layer.
package transport

import (
	"context"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
)

// NodeRef is a reference to a remote node.
type NodeRef struct {
	ID   [20]byte
	Addr string
}

// HopEvent records a single routing hop during a lookup.
type HopEvent struct {
	FromNode  string
	ToNode    string
	Mechanism string // e.g., "finger[7]", "successor", "k-bucket[3]"
	HopIndex  int
}

// FindValueResult holds the result of a Kademlia FIND_VALUE RPC.
type FindValueResult struct {
	Found    bool
	Entry    *store.ValueEntry
	Contacts []NodeRef
}

// Transport abstracts all inter-node RPCs for both Chord and Kademlia.
type Transport interface {
	// Chord RPCs
	FindSuccessor(ctx context.Context, target NodeRef, id [20]byte) (NodeRef, error)
	GetPredecessor(ctx context.Context, target NodeRef) (*NodeRef, error)
	Notify(ctx context.Context, target NodeRef, sender NodeRef) error
	GetSuccessorList(ctx context.Context, target NodeRef) ([]NodeRef, error)
	TransferKeys(ctx context.Context, target NodeRef, entries []*store.ValueEntry) error
	UpdateFingerTable(ctx context.Context, target NodeRef, s NodeRef, i int) error

	// Kademlia RPCs — sender identifies the calling node for routing-table updates.
	KPing(ctx context.Context, sender NodeRef, target NodeRef) error
	KStore(ctx context.Context, sender NodeRef, target NodeRef, entry *store.ValueEntry) error
	FindNode(ctx context.Context, sender NodeRef, target NodeRef, id [20]byte) ([]NodeRef, error)
	FindValue(ctx context.Context, sender NodeRef, target NodeRef, key [20]byte) (*FindValueResult, error)

	// Common
	Ping(ctx context.Context, target NodeRef) error
	GetEntry(ctx context.Context, target NodeRef, key [20]byte) (*store.ValueEntry, error)
	PutEntry(ctx context.Context, target NodeRef, key [20]byte, entry *store.ValueEntry) error
	GetAllEntries(ctx context.Context, target NodeRef) ([]*store.ValueEntry, error)
}

// NodeHandler is the interface that each DHT node must implement
// to handle incoming RPCs from the transport layer.
type NodeHandler interface {
	// Chord handlers
	HandleFindSuccessor(ctx context.Context, id [20]byte) (NodeRef, error)
	HandleGetPredecessor(ctx context.Context) (*NodeRef, error)
	HandleNotify(ctx context.Context, sender NodeRef) error
	HandleGetSuccessorList(ctx context.Context) ([]NodeRef, error)
	HandleTransferKeys(ctx context.Context, entries []*store.ValueEntry) error
	HandleUpdateFingerTable(ctx context.Context, s NodeRef, i int) error

	// Kademlia handlers
	HandleKPing(ctx context.Context, sender NodeRef) error
	HandleKStore(ctx context.Context, sender NodeRef, entry *store.ValueEntry) error
	HandleFindNode(ctx context.Context, sender NodeRef, id [20]byte) ([]NodeRef, error)
	HandleFindValue(ctx context.Context, sender NodeRef, key [20]byte) (*FindValueResult, error)

	// Common handlers
	HandlePing(ctx context.Context) error
	HandleGetEntry(ctx context.Context, key [20]byte) (*store.ValueEntry, error)
	HandlePutEntry(ctx context.Context, key [20]byte, entry *store.ValueEntry) error
	HandleGetAllEntries(ctx context.Context) ([]*store.ValueEntry, error)
}
