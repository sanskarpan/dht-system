// Package chord implements the Chord distributed hash table protocol.
//
// Chord organizes participating nodes on a 160-bit circular identifier space
// (the "ring"). Each node is responsible for a contiguous arc of key space
// between its predecessor and itself.  Keys are mapped to their successor node
// in O(log N) hops using a finger table of 160 shortcuts: finger[i] points to
// the successor of (self + 2^i) mod 2^160.
//
// Background goroutines run stabilize, fix-fingers, and check-predecessor /
// check-successor cycles to maintain ring consistency after joins and crashes.
// Replication is handled externally by the QuorumManager.
package chord

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

const fingerBits = 160 // m = 160 bits

// Config holds all Chord tuning parameters.
type Config struct {
	StabilizeInterval  time.Duration
	FixFingersInterval time.Duration
	CheckPredInterval  time.Duration
	CheckSuccInterval  time.Duration
	SuccessorListSize  int
	MaxHops            int // default 3*fingerBits
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		StabilizeInterval:  500 * time.Millisecond,
		FixFingersInterval: 1 * time.Second,
		CheckPredInterval:  750 * time.Millisecond,
		CheckSuccInterval:  750 * time.Millisecond,
		SuccessorListSize:  8,
		MaxHops:            3 * fingerBits,
	}
}

// ChordNode is a node in the Chord ring.
type ChordNode struct {
	ID   [20]byte
	Addr string

	mu          sync.RWMutex
	successor   *transport.NodeRef // same as fingers[0]
	predecessor *transport.NodeRef
	fingers      [fingerBits]*transport.NodeRef
	antiFingers  [fingerBits]*transport.NodeRef // antiFinger[i] = predecessor of (n.ID - 2^i) mod 2^160
	antiFingerIdx int                           // next anti-finger index to refresh
	succList    []*transport.NodeRef

	Store     *store.KVStore
	Transport transport.Transport
	Bus       events.EventEmitter
	Config    *Config
	Logger    *zap.Logger

	stopCh chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
}

// NewChordNode creates a new ChordNode (not yet joined/started).
func NewChordNode(
	addr string,
	cfg *Config,
	t transport.Transport,
	bus events.EventEmitter,
	logger *zap.Logger,
) *ChordNode {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	id := consistent.NodeIDFromAddr(addr)
	ctx, cancel := context.WithCancel(context.Background())
	n := &ChordNode{
		ID:        id,
		Addr:      addr,
		Store:     store.NewKVStore(),
		Transport: t,
		Bus:       bus,
		Config:    cfg,
		Logger:    logger.With(zap.String("node", consistent.IDToHex(id)[:8])),
		stopCh:    make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
	}
	// Initialize: point successor to self so the node is trivially valid.
	self := n.selfRef()
	n.successor = &self
	return n
}

func (n *ChordNode) selfRef() transport.NodeRef {
	return transport.NodeRef{ID: n.ID, Addr: n.Addr}
}

// Successor returns the current successor (thread-safe).
func (n *ChordNode) Successor() *transport.NodeRef {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.successor
}

// Predecessor returns the current predecessor (thread-safe).
func (n *ChordNode) Predecessor() *transport.NodeRef {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.predecessor
}

// SetSuccessor sets the successor (thread-safe).
func (n *ChordNode) SetSuccessor(ref *transport.NodeRef) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.successor = ref
	if ref != nil {
		n.fingers[0] = ref
	}
}

// SetPredecessor sets the predecessor (thread-safe).
func (n *ChordNode) SetPredecessor(ref *transport.NodeRef) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.predecessor = ref
}

// Start launches all background goroutines (stabilize, check_pred, check_succ).
// Fix-fingers runs inside the runStabilize goroutine via a separate ticker.
func (n *ChordNode) Start() {
	n.Store.StartTTLEviction(n.ctx, 10*time.Second)
	go n.runStabilize()
	go n.runCheckPredecessor()
	go n.runCheckSuccessor()
}

// Stop halts all background goroutines and closes the node.
func (n *ChordNode) Stop() {
	n.cancel()
	// Guard against double-close.
	select {
	case <-n.stopCh:
	default:
		close(n.stopCh)
	}
}

// IDHex returns the hex-encoded node ID.
func (n *ChordNode) IDHex() string {
	return consistent.IDToHex(n.ID)
}

// Ref returns a NodeRef for this node.
func (n *ChordNode) Ref() transport.NodeRef {
	return n.selfRef()
}

// SuccessorList returns a copy of the successor list.
func (n *ChordNode) SuccessorList() []*transport.NodeRef {
	n.mu.RLock()
	defer n.mu.RUnlock()
	cp := make([]*transport.NodeRef, len(n.succList))
	copy(cp, n.succList)
	return cp
}

// GetFinger returns the finger at index i.
func (n *ChordNode) GetFinger(i int) *transport.NodeRef {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.fingers[i]
}

// SetFinger sets the finger at index i.
func (n *ChordNode) SetFinger(i int, ref *transport.NodeRef) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.fingers[i] = ref
	if i == 0 && ref != nil {
		n.successor = ref
	}
}

// fingerStart returns (n.ID + 2^i) mod 2^160.
func (n *ChordNode) fingerStart(i int) [20]byte {
	return consistent.Add(n.ID, consistent.PowerOfTwo(i))
}

// Snapshot returns a human-readable snapshot of node state.
func (n *ChordNode) Snapshot() map[string]interface{} {
	n.mu.RLock()
	defer n.mu.RUnlock()
	m := map[string]interface{}{
		"id":   consistent.IDToHex(n.ID),
		"addr": n.Addr,
	}
	if n.successor != nil {
		m["successor"] = consistent.IDToHex(n.successor.ID)
	}
	if n.predecessor != nil {
		m["predecessor"] = consistent.IDToHex(n.predecessor.ID)
	}
	return m
}

// ensure fmt is used
var _ = fmt.Sprintf
