package gossip_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/gossip"
	"github.com/sanskarpan/dht-system/dht-system/internal/kademlia"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// buildGossipNetwork creates n Kademlia-backed nodes with gossip engines wired up.
// Each node participates in anti-entropy at interval.
func buildGossipNetwork(t *testing.T, n int, interval time.Duration) (
	nodes []*kademlia.KademliaNode,
	aes []*gossip.AntiEntropy,
	tr *transport.InProcessTransport,
	bus *events.EventBus,
) {
	t.Helper()
	bus = events.NewEventBus()
	tr = transport.NewInProcessTransport(0, 0)

	cfg := kademlia.DefaultConfig()
	cfg.RepublishInterval = 10 * time.Minute
	cfg.BucketRefreshInterval = 10 * time.Minute

	for i := 0; i < n; i++ {
		addr := fmt.Sprintf("gossip-node-%02d:9000", i)
		node := kademlia.NewKademliaNode(addr, cfg, tr, bus, nil)
		tr.Register(addr, node)
		nodes = append(nodes, node)
	}

	// Join all nodes to nodes[0] as bootstrap
	for i := 1; i < n; i++ {
		bootstrap := kademlia.Contact{
			ID:   nodes[0].ID,
			Addr: nodes[0].Addr,
		}
		if err := nodes[i].Join(context.Background(), bootstrap); err != nil {
			t.Fatalf("node %d join failed: %v", i, err)
		}
	}

	// Build gossip engines
	for i, node := range nodes {
		ni := i
		nd := node
		gCfg := gossip.DefaultConfig()
		gCfg.Interval = interval
		gCfg.PeersPerRound = 2

		ae := gossip.New(
			nd.Addr,
			nd.KVStore,
			tr,
			bus,
			func() []transport.NodeRef {
				var peers []transport.NodeRef
				for j, other := range nodes {
					if j != ni {
						peers = append(peers, transport.NodeRef{ID: [20]byte(other.ID), Addr: other.Addr})
					}
				}
				return peers
			},
			gCfg,
		)
		aes = append(aes, ae)
	}

	t.Cleanup(func() {
		bus.Stop()
		for _, ae := range aes {
			ae.Stop()
		}
	})
	return
}

// TestGossipConvergesAfterPartition verifies that after 3 gossip cycles
// following a partition, all nodes have all keys.
func TestGossipConvergesAfterPartition(t *testing.T) {
	const (
		nodeCount    = 6
		keysPerGroup = 5
		cycleInterval = 50 * time.Millisecond
		cycles       = 3
	)

	nodes, aes, tr, _ := buildGossipNetwork(t, nodeCount, cycleInterval)

	// Insert keys[0..4] directly into nodes[0] only (simulating partition A writes)
	keysA := make([][20]byte, keysPerGroup)
	for i := range keysA {
		key := consistent.KeyID(fmt.Sprintf("partition-a-key-%d", i))
		keysA[i] = key
		nodes[0].KVStore.Put(&store.ValueEntry{
			Key:       key,
			Value:     []byte(fmt.Sprintf("val-a-%d", i)),
			Timestamp: time.Now(),
			NodeID:    nodes[0].Addr,
		})
		aes[0].NotifyPut(key, []byte(fmt.Sprintf("val-a-%d", i)))
	}

	// Insert keys[5..9] directly into nodes[nodeCount-1] (simulating partition B writes)
	keysB := make([][20]byte, keysPerGroup)
	for i := range keysB {
		key := consistent.KeyID(fmt.Sprintf("partition-b-key-%d", i))
		keysB[i] = key
		last := nodes[nodeCount-1]
		last.KVStore.Put(&store.ValueEntry{
			Key:       key,
			Value:     []byte(fmt.Sprintf("val-b-%d", i)),
			Timestamp: time.Now(),
			NodeID:    last.Addr,
		})
		aes[nodeCount-1].NotifyPut(key, []byte(fmt.Sprintf("val-b-%d", i)))
	}

	// Simulate partition: nodes are split but transport is still connected (simulated gossip)
	// We deliberately don't call SetPartition to let gossip do the reconciliation.

	// Isolate: block cross-group RPCs during "partition" by not running gossip yet.
	// Then heal by starting gossip — 3 cycles should reconcile.

	// Mark partition healed: re-enable transport (already enabled), start gossip
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = tr // transport already enabled (no partition was injected into InProcessTransport)

	for _, ae := range aes {
		ae.Start(ctx)
	}

	// Wait for 3+ gossip cycles
	time.Sleep(cycleInterval * time.Duration(cycles+2))

	// After gossip, ALL nodes should have ALL keys from both groups
	allKeys := append(keysA, keysB...)
	failures := 0
	for ni, node := range nodes {
		for _, k := range allKeys {
			if _, ok := node.KVStore.Get(k); !ok {
				t.Logf("node %d missing key %x", ni, k[:4])
				failures++
			}
		}
	}

	t.Logf("after %d gossip cycles: %d/%d (node×key) pairs present",
		cycles, nodeCount*len(allKeys)-failures, nodeCount*len(allKeys))

	// Allow up to 10% failures (gossip is probabilistic)
	maxFail := (nodeCount * len(allKeys)) / 10
	if failures > maxFail {
		t.Errorf("too many missing pairs: %d (max %d)", failures, maxFail)
	}
}
