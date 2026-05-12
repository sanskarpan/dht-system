// Package gossip implements an anti-entropy background goroutine that
// reconciles key-value entries between DHT nodes using Merkle tree root
// comparison followed by a full key diff.
package gossip

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// Config holds AntiEntropy tuning parameters.
type Config struct {
	// Interval between gossip rounds (default 5s).
	Interval time.Duration
	// PeersPerRound is how many random peers to sync per interval (default 2).
	PeersPerRound int
}

func DefaultConfig() *Config {
	return &Config{
		Interval:      5 * time.Second,
		PeersPerRound: 2,
	}
}

// AntiEntropy continuously reconciles the local store with random peers.
type AntiEntropy struct {
	localAddr   string
	localStore  *store.KVStore
	transport   transport.Transport
	bus         events.EventEmitter
	getPeers    func() []transport.NodeRef
	cfg         *Config
	localMerkle *store.MerkleTree

	mu     sync.Mutex
	stopCh chan struct{}
}

// New creates an AntiEntropy engine.
//   - localAddr  – address of this node (for event emission)
//   - localStore – the node's KV store
//   - t          – transport to call GetAllEntries / PutEntry on peers
//   - bus        – event bus for gossip_sync events
//   - getPeers   – function that returns the current live peer list
//   - cfg        – nil → DefaultConfig()
func New(
	localAddr string,
	localStore *store.KVStore,
	t transport.Transport,
	bus events.EventEmitter,
	getPeers func() []transport.NodeRef,
	cfg *Config,
) *AntiEntropy {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	ae := &AntiEntropy{
		localAddr:   localAddr,
		localStore:  localStore,
		transport:   t,
		bus:         bus,
		getPeers:    getPeers,
		cfg:         cfg,
		localMerkle: store.NewMerkleTree(),
		stopCh:      make(chan struct{}),
	}
	// Build initial Merkle tree from existing entries
	for _, e := range localStore.All() {
		ae.localMerkle.Update(e.Key, e.Value)
	}
	return ae
}

// Start launches the background goroutine.
func (ae *AntiEntropy) Start(ctx context.Context) {
	go ae.run(ctx)
}

// Stop halts the background goroutine.
func (ae *AntiEntropy) Stop() {
	select {
	case <-ae.stopCh:
	default:
		close(ae.stopCh)
	}
}

// NotifyPut should be called whenever a key is written to the local store
// so the Merkle tree stays in sync.
func (ae *AntiEntropy) NotifyPut(key [20]byte, value []byte) {
	ae.mu.Lock()
	ae.localMerkle.Update(key, value)
	ae.mu.Unlock()
}

// NotifyDelete should be called whenever a key is deleted from the local store.
func (ae *AntiEntropy) NotifyDelete(key [20]byte) {
	ae.mu.Lock()
	ae.localMerkle.Delete(key)
	ae.mu.Unlock()
}

func (ae *AntiEntropy) run(ctx context.Context) {
	ticker := time.NewTicker(ae.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ae.stopCh:
			return
		case <-ticker.C:
			ae.gossipRound(ctx)
		}
	}
}

func (ae *AntiEntropy) gossipRound(ctx context.Context) {
	ae.rebuildMerkleFromStore()
	peers := ae.getPeers()
	if len(peers) == 0 {
		return
	}

	// Shuffle and pick up to PeersPerRound
	rand.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
	if len(peers) > ae.cfg.PeersPerRound {
		peers = peers[:ae.cfg.PeersPerRound]
	}

	for _, peer := range peers {
		ae.syncWithPeer(ctx, peer)
	}
}

func (ae *AntiEntropy) rebuildMerkleFromStore() {
	tree := store.NewMerkleTree()
	for _, entry := range ae.localStore.All() {
		tree.Update(entry.Key, entry.Value)
	}
	ae.mu.Lock()
	ae.localMerkle = tree
	ae.mu.Unlock()
}

// syncWithPeer runs one bilateral anti-entropy exchange with the given peer.
//
// Protocol:
//  1. Fetch all entries from peer via GetAllEntries RPC.
//  2. Build a MerkleTree over peer entries; compare its root to ours.
//  3. If roots match, both stores are identical — return immediately (no-op).
//  4. Compute Diff(peerTree) to get the set of keys that differ.
//  5. For each differing key: push local copy to peer, pull peer copy locally,
//     or resolve conflicts using LWW (last-write-wins by timestamp).
//  6. Emit a gossip_sync event with reconciliation statistics.
func (ae *AntiEntropy) syncWithPeer(ctx context.Context, peer transport.NodeRef) {
	localRef := transport.NodeRef{ID: consistent.NodeIDFromAddr(ae.localAddr), Addr: ae.localAddr}
	ctx = transport.WithSender(ctx, localRef)

	// 1. Fetch all entries from the peer
	peerEntries, err := ae.transport.GetAllEntries(ctx, peer)
	if err != nil {
		return
	}

	// 2. Build a Merkle tree for the peer's entries
	peerMerkle := store.NewMerkleTree()
	peerMap := make(map[[20]byte]*store.ValueEntry, len(peerEntries))
	for _, e := range peerEntries {
		peerMerkle.Update(e.Key, e.Value)
		peerMap[e.Key] = e
	}

	// 3. If roots match, no work needed
	ae.mu.Lock()
	localRoot := ae.localMerkle.Root()
	ae.mu.Unlock()

	peerRoot := peerMerkle.Root()
	if localRoot == peerRoot {
		return
	}

	// 4. Diff: find keys that differ
	ae.mu.Lock()
	diff := ae.localMerkle.Diff(peerMerkle)
	ae.mu.Unlock()

	divergentBefore := len(diff)
	reconciled := 0

	for _, key := range diff {
		localEntry, localOK := ae.localStore.Get(key)
		peerEntry, peerOK := peerMap[key]

		switch {
		case localOK && !peerOK:
			// We have it, peer doesn't → push to peer
			_ = ae.transport.PutEntry(ctx, peer, key, localEntry)
			reconciled++

		case !localOK && peerOK:
			// Peer has it, we don't → store locally
			ae.localStore.Put(peerEntry)
			ae.mu.Lock()
			ae.localMerkle.Update(peerEntry.Key, peerEntry.Value)
			ae.mu.Unlock()
			reconciled++

		case localOK && peerOK:
			// Both have it: pick winner by vector clock / LWW
			winner := pickWinner(localEntry, peerEntry)
			if winner == peerEntry {
				ae.localStore.Put(peerEntry)
				ae.mu.Lock()
				ae.localMerkle.Update(peerEntry.Key, peerEntry.Value)
				ae.mu.Unlock()
			} else {
				_ = ae.transport.PutEntry(ctx, peer, key, localEntry)
			}
			reconciled++
		}
	}

	if ae.bus != nil {
		ae.bus.Publish(events.MakeEvent(events.EventGossipSync, events.GossipSyncPayload{
			FromNode:        consistent.IDToHex(consistent.NodeIDFromAddr(ae.localAddr)),
			ToNode:          consistent.IDToHex(peer.ID),
			KeysReconciled:  reconciled,
			DivergentBefore: divergentBefore,
			DivergentAfter:  divergentBefore - reconciled,
		}))
	}
}

// pickWinner applies LWW (last-write-wins by timestamp) to choose between two entries.
func pickWinner(a, b *store.ValueEntry) *store.ValueEntry {
	if b.Timestamp.After(a.Timestamp) {
		return b
	}
	return a
}
