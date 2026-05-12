// Package kademlia implements the Kademlia distributed hash table protocol.
//
// Kademlia uses XOR distance as its metric: the distance between two node IDs
// a and b is a XOR b (interpreted as an unsigned integer).  Each node maintains
// a routing table of 160 k-buckets; bucket i holds contacts at XOR distance
// in [2^i, 2^(i+1)).
//
// Lookups are iterative: the initiator sends α (default 3) parallel FIND_NODE
// RPCs to the α closest known contacts, merges the results, and repeats until
// the k closest nodes to the target are fully queried.  FIND_VALUE works the
// same way but stops early when a node returns the requested value.
//
// Values are stored on the k closest nodes and republished every RepublishInterval
// to prevent data loss due to churn.  Looked-up values are cached on the closest
// node that did not already hold them (§2.3 of the original paper).
package kademlia

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// Config holds Kademlia tuning parameters.
type Config struct {
	K                     int           // bucket size / replication factor, default 20
	Alpha                 int           // concurrency parameter, default 3
	AlphaTimeout          time.Duration // per-iteration RPC timeout, default 500ms
	RepublishInterval     time.Duration // default 24s (demo)
	ExpireTTL             time.Duration // default 25s (demo)
	BucketRefreshInterval time.Duration // default 60s
	PingTimeout           time.Duration // default 300ms
}

func DefaultConfig() *Config {
	return &Config{
		K:                     20,
		Alpha:                 3,
		AlphaTimeout:          500 * time.Millisecond,
		RepublishInterval:     24 * time.Second,
		ExpireTTL:             25 * time.Second,
		BucketRefreshInterval: 60 * time.Second,
		PingTimeout:           300 * time.Millisecond,
	}
}

// KademliaNode is a node in a Kademlia DHT.
type KademliaNode struct {
	ID           NodeID
	Addr         string
	RoutingTable *RoutingTable
	KVStore      *store.KVStore
	Transport    transport.Transport
	Bus          events.EventEmitter
	Config       *Config
	Logger       *zap.Logger

	mu     sync.RWMutex
	stopCh chan struct{}
	ctx    context.Context
	cancel context.CancelFunc

	antiEntropy AntiEntropyEngine
}

type AntiEntropyEngine interface {
	Start(ctx context.Context)
	Stop()
	NotifyPut(key [20]byte, value []byte)
	NotifyDelete(key [20]byte)
}

// NewKademliaNode creates a new KademliaNode (not yet joined).
func NewKademliaNode(addr string, cfg *Config, t transport.Transport, bus events.EventEmitter, logger *zap.Logger) *KademliaNode {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	id := NodeID(consistent.NodeIDFromAddr(addr))
	ctx, cancel := context.WithCancel(context.Background())
	return &KademliaNode{
		ID:           id,
		Addr:         addr,
		RoutingTable: NewRoutingTable(id, cfg.K),
		KVStore:      store.NewKVStore(),
		Transport:    t,
		Bus:          bus,
		Config:       cfg,
		Logger:       logger.With(zap.String("node", consistent.IDToHex([20]byte(id))[:8])),
		stopCh:       make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Ref returns a NodeRef for this node.
func (n *KademliaNode) Ref() transport.NodeRef {
	return transport.NodeRef{ID: [20]byte(n.ID), Addr: n.Addr}
}

// Start launches background goroutines.
func (n *KademliaNode) Start() {
	n.KVStore.StartTTLEviction(n.ctx, 10*time.Second)
	if n.antiEntropy != nil {
		n.antiEntropy.Start(n.ctx)
	}
	go n.runRepublish()
	go n.runBucketRefresh()
}

// Stop halts all background goroutines.
func (n *KademliaNode) Stop() {
	n.cancel()
	if n.antiEntropy != nil {
		n.antiEntropy.Stop()
	}
	select {
	case <-n.stopCh:
	default:
		close(n.stopCh)
	}
}

func (n *KademliaNode) SetAntiEntropy(ae AntiEntropyEngine) {
	n.antiEntropy = ae
}

// updateRoutingTable adds/updates a contact in the routing table.
func (n *KademliaNode) updateRoutingTable(c Contact) {
	pingFn := func(contact Contact) error {
		ctx, cancel := context.WithTimeout(n.ctx, n.Config.PingTimeout)
		defer cancel()
		return n.Transport.KPing(ctx, n.Ref(), transport.NodeRef{ID: [20]byte(contact.ID), Addr: contact.Addr})
	}
	idx, updated := n.RoutingTable.UpdateContact(c, pingFn)
	if updated && n.Bus != nil {
		n.Bus.Publish(events.MakeEvent(events.EventBucketUpdate, events.BucketUpdatePayload{
			NodeID:      consistent.IDToHex([20]byte(n.ID)),
			BucketIndex: idx,
			ContactID:   consistent.IDToHex([20]byte(c.ID)),
			ContactAddr: c.Addr,
		}))
	}
}

func (n *KademliaNode) runRepublish() {
	ticker := time.NewTicker(n.Config.RepublishInterval)
	defer ticker.Stop()
	for {
		select {
		case <-n.stopCh:
			return
		case <-ticker.C:
			n.republishKeys()
		}
	}
}

func (n *KademliaNode) runBucketRefresh() {
	ticker := time.NewTicker(n.Config.BucketRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-n.stopCh:
			return
		case <-ticker.C:
			n.refreshBuckets()
		}
	}
}

func (n *KademliaNode) republishKeys() {
	entries := n.KVStore.All()
	for _, e := range entries {
		key := NodeID(e.Key)
		contacts, _, err := n.IterativeFindNode(n.ctx, key)
		if err != nil {
			continue
		}
		for _, c := range contacts {
			ctx, cancel := context.WithTimeout(n.ctx, n.Config.PingTimeout)
			_ = n.Transport.KStore(ctx, n.Ref(), transport.NodeRef{ID: [20]byte(c.ID), Addr: c.Addr}, e)
			cancel()
		}
		n.Bus.Publish(events.MakeEvent(events.EventRepublish, events.RepublishPayload{
			Key:    consistent.IDToHex([20]byte(key)),
			NodeID: consistent.IDToHex([20]byte(n.ID)),
		}))
	}
}

func (n *KademliaNode) refreshBuckets() {
	for i := range n.RoutingTable.Buckets {
		if n.RoutingTable.Buckets[i].Len() == 0 {
			// Generate a random ID in this bucket's range
			// Bucket i covers XOR distances in [2^i, 2^(i+1))
			// Any ID that differs from self at bit position i
			var randomID NodeID
			copy(randomID[:], n.ID[:])
			// Flip bit i
			byteIdx := 19 - i/8
			bitIdx := uint(i % 8)
			if byteIdx >= 0 {
				randomID[byteIdx] ^= (1 << bitIdx)
			}
			_, _, _ = n.IterativeFindNode(n.ctx, randomID)
		}
	}
}
