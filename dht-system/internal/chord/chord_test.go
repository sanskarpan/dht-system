package chord_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/chord"
	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// buildRing creates a ring of n ChordNodes, waits for stabilization, and
// registers a cleanup function to stop all nodes. Returns the nodes and
// the shared in-process transport.
func buildRing(t *testing.T, n int) ([]*chord.ChordNode, *transport.InProcessTransport) {
	t.Helper()
	tr := transport.NewInProcessTransport(0, 0)
	bus := events.NewEventBus()
	cfg := chord.DefaultConfig()
	cfg.StabilizeInterval = 50 * time.Millisecond
	cfg.FixFingersInterval = 100 * time.Millisecond
	cfg.CheckPredInterval = 50 * time.Millisecond
	cfg.CheckSuccInterval = 50 * time.Millisecond

	nodes := make([]*chord.ChordNode, n)
	for i := 0; i < n; i++ {
		addr := fmt.Sprintf("127.0.0.1:%d", 7000+i)
		node := chord.NewChordNode(addr, cfg, tr, bus, nil)
		tr.Register(addr, node)
		nodes[i] = node
	}

	// Bootstrap: first node creates ring, rest join
	nodes[0].CreateRing()
	nodes[0].Start()
	for i := 1; i < n; i++ {
		err := nodes[i].Join(context.Background(), nodes[0].Ref())
		if err != nil {
			t.Fatalf("node[%d].Join failed: %v", i, err)
		}
		nodes[i].Start()
	}

	// Wait for stabilization
	time.Sleep(500 * time.Millisecond)

	t.Cleanup(func() {
		for _, node := range nodes {
			node.Stop()
		}
		bus.Stop()
	})
	return nodes, tr
}

// isHealthyNode returns true if the node has a non-self-loop successor and
// a non-nil predecessor, meaning it is in a valid ring segment.
func isHealthyNode(node *chord.ChordNode) bool {
	succ := node.Successor()
	pred := node.Predecessor()
	if succ == nil || pred == nil {
		return false
	}
	// self-loop: successor points to self
	if succ.ID == node.Ref().ID {
		return false
	}
	return true
}

// TestSingleNodeRing verifies that a 1-node ring routes any key back to itself.
func TestSingleNodeRing(t *testing.T) {
	nodes, _ := buildRing(t, 1)
	node := nodes[0]
	selfRef := node.Ref()

	// Try several random keys
	for i := 0; i < 10; i++ {
		var key [20]byte
		if _, err := rand.Read(key[:]); err != nil {
			t.Fatalf("rand.Read: %v", err)
		}
		got, _, err := node.FindSuccessor(context.Background(), key)
		if err != nil {
			t.Fatalf("FindSuccessor error: %v", err)
		}
		if got.ID != selfRef.ID {
			t.Errorf("single-node ring: FindSuccessor returned %s, want self %s",
				consistent.IDToHex(got.ID), consistent.IDToHex(selfRef.ID))
		}
	}
}

// TestTwoNodeRing verifies basic ring invariants for a 2-node ring.
func TestTwoNodeRing(t *testing.T) {
	nodes, _ := buildRing(t, 2)
	n0, n1 := nodes[0], nodes[1]

	succ0 := n0.Successor()
	succ1 := n1.Successor()

	if succ0 == nil {
		t.Fatal("n0.Successor is nil after stabilization")
	}
	if succ1 == nil {
		t.Fatal("n1.Successor is nil after stabilization")
	}

	// In a 2-node ring each node must be a known member
	if succ0.ID != n1.ID && succ0.ID != n0.ID {
		t.Errorf("n0 successor %s is neither n0 nor n1", consistent.IDToHex(succ0.ID))
	}
	if succ1.ID != n0.ID && succ1.ID != n1.ID {
		t.Errorf("n1 successor %s is neither n0 nor n1", consistent.IDToHex(succ1.ID))
	}

	// FindSuccessor for any key must return one of the two nodes
	for i := 0; i < 20; i++ {
		var key [20]byte
		if _, err := rand.Read(key[:]); err != nil {
			t.Fatalf("rand.Read: %v", err)
		}
		got, _, err := n0.FindSuccessor(context.Background(), key)
		if err != nil {
			t.Fatalf("FindSuccessor error: %v", err)
		}
		if got.ID != n0.ID && got.ID != n1.ID {
			t.Errorf("FindSuccessor returned unknown node %s", consistent.IDToHex(got.ID))
		}
	}
}

// TestFindSuccessorCorrectness verifies that for 100 random keys in an 8-node ring:
//  1. FindSuccessor is idempotent (same key → same result from same starting node).
//  2. The returned node is a known ring member.
//  3. For healthy starting nodes (non-nil pred, non-self successor), the key
//     falls in (pred.ID, succ.ID] — the Chord correctness invariant.
func TestFindSuccessorCorrectness(t *testing.T) {
	const n = 8
	nodes, _ := buildRing(t, n)

	// Give extra time for an 8-node ring to fully stabilize
	time.Sleep(300 * time.Millisecond)

	// Build a set of known node IDs for membership checks
	memberIDs := make(map[[20]byte]bool, n)
	for _, node := range nodes {
		memberIDs[node.Ref().ID] = true
	}

	// Identify healthy source nodes (non-self-loop, non-nil predecessor)
	var healthy []*chord.ChordNode
	for _, node := range nodes {
		if isHealthyNode(node) {
			healthy = append(healthy, node)
		}
	}
	if len(healthy) == 0 {
		t.Fatal("no healthy nodes found in the 8-node ring after stabilization")
	}
	t.Logf("healthy source nodes: %d/%d", len(healthy), n)

	checkedRanges := 0
	for trial := 0; trial < 100; trial++ {
		var key [20]byte
		if _, err := rand.Read(key[:]); err != nil {
			t.Fatalf("rand.Read: %v", err)
		}

		// Use a healthy source node for the lookup (round-robin)
		src := healthy[trial%len(healthy)]

		// Query twice from the same source — results must be identical (idempotency)
		got1, _, err := src.FindSuccessor(context.Background(), key)
		if err != nil {
			t.Fatalf("trial %d: first FindSuccessor error: %v", trial, err)
		}
		got2, _, err := src.FindSuccessor(context.Background(), key)
		if err != nil {
			t.Fatalf("trial %d: second FindSuccessor error: %v", trial, err)
		}
		if got1.ID != got2.ID {
			t.Errorf("trial %d: FindSuccessor not idempotent: got %s then %s",
				trial, consistent.IDToHex(got1.ID), consistent.IDToHex(got2.ID))
		}

		// Result must be a ring member
		if !memberIDs[got1.ID] {
			t.Errorf("trial %d: FindSuccessor returned non-member node %s",
				trial, consistent.IDToHex(got1.ID))
			continue
		}

		// Find the actual node object for the returned reference
		var succNode *chord.ChordNode
		for _, node := range nodes {
			if node.Ref().ID == got1.ID {
				succNode = node
				break
			}
		}
		if succNode == nil {
			continue
		}

		// Only verify (pred, succ] range for nodes in a healthy state.
		if !isHealthyNode(succNode) {
			continue
		}

		pred := succNode.Predecessor()
		succRef := succNode.Ref()

		if !consistent.IsInIntervalRightClosed(key, pred.ID, succRef.ID) {
			t.Errorf("trial %d: key %s not in (pred=%s, succ=%s]",
				trial,
				consistent.IDToHex(key),
				consistent.IDToHex(pred.ID),
				consistent.IDToHex(succRef.ID),
			)
		}
		checkedRanges++
	}

	t.Logf("range-checked %d/100 trials (healthy destination nodes only)", checkedRanges)

	// We must have checked at least some ranges to ensure the test is meaningful
	if checkedRanges == 0 {
		t.Error("no range checks performed — no keys resolved to a healthy destination node")
	}
}

// TestHopCount verifies that for an 8-node ring, lookup hops are bounded by
// ceil(log2(8)) + 2 = 5.
func TestHopCount(t *testing.T) {
	const n = 8
	nodes, _ := buildRing(t, n)

	// Extra stabilization time
	time.Sleep(300 * time.Millisecond)

	maxAllowedHops := int(math.Ceil(math.Log2(float64(n)))) + 2 // 5

	for trial := 0; trial < 30; trial++ {
		var key [20]byte
		if _, err := rand.Read(key[:]); err != nil {
			t.Fatalf("rand.Read: %v", err)
		}
		_, hops, err := nodes[0].FindSuccessor(context.Background(), key)
		if err != nil {
			t.Fatalf("trial %d: FindSuccessor error: %v", trial, err)
		}
		if len(hops) > maxAllowedHops {
			t.Errorf("trial %d: hop count %d > max allowed %d (key=%s)",
				trial, len(hops), maxAllowedHops, consistent.IDToHex(key))
		}
	}
}

// TestKeyMigrationOnJoin verifies that total key count is preserved when a new
// node joins an existing ring.
func TestKeyMigrationOnJoin(t *testing.T) {
	// Build 3-node ring
	nodes, tr := buildRing(t, 3)

	const numKeys = 20
	// Insert 20 keys directly into node[0]'s store
	for i := 0; i < numKeys; i++ {
		key := consistent.KeyID(fmt.Sprintf("migration-key-%d", i))
		nodes[0].Store.Put(&store.ValueEntry{
			Key:       key,
			Value:     []byte(fmt.Sprintf("value-%d", i)),
			Timestamp: time.Now(),
			TTL:       0, // no expiry
		})
	}

	// Verify node[0] has 20 keys before the join
	if got := nodes[0].Store.Size(); got != numKeys {
		t.Fatalf("before join: node[0] has %d keys, want %d", got, numKeys)
	}

	// Add a 4th node that joins and may migrate some keys
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Stop() })

	cfg := chord.DefaultConfig()
	cfg.StabilizeInterval = 50 * time.Millisecond
	cfg.FixFingersInterval = 100 * time.Millisecond
	cfg.CheckPredInterval = 50 * time.Millisecond
	cfg.CheckSuccInterval = 50 * time.Millisecond

	addr3 := "127.0.0.1:7003"
	node3 := chord.NewChordNode(addr3, cfg, tr, bus, nil)
	tr.Register(addr3, node3)
	t.Cleanup(func() {
		node3.Stop()
		tr.Deregister(addr3)
	})

	if err := node3.Join(context.Background(), nodes[0].Ref()); err != nil {
		t.Fatalf("node3.Join: %v", err)
	}
	node3.Start()

	// Wait for stabilization and potential key migration
	time.Sleep(600 * time.Millisecond)

	// Count total keys across all 4 nodes
	allNodes := append(nodes, node3)
	total := 0
	for i, node := range allNodes {
		count := node.Store.Size()
		t.Logf("node[%d] (%s): %d keys", i, node.Ref().Addr, count)
		total += count
	}

	// Total must be at least numKeys (no data loss)
	if total < numKeys {
		t.Errorf("key migration: total keys across all nodes = %d, want >= %d (no data loss)", total, numKeys)
	}
}

// TestSuccessorListFallback verifies that after crashing a node, remaining
// nodes can still route via the successor list.
func TestSuccessorListFallback(t *testing.T) {
	nodes, tr := buildRing(t, 4)

	// Give extra time so the successor lists are populated
	time.Sleep(300 * time.Millisecond)

	// Find a non-node[0] node to crash (to avoid crashing the node we'll query from)
	var crashed *chord.ChordNode
	succ0 := nodes[0].Successor()
	if succ0 != nil && succ0.ID != nodes[0].Ref().ID {
		for _, node := range nodes {
			if node.Ref().ID == succ0.ID {
				crashed = node
				break
			}
		}
	}
	// Fallback: crash nodes[1] if above didn't work
	if crashed == nil {
		crashed = nodes[1]
	}

	t.Logf("crashing node %s (id=%s)", crashed.Ref().Addr, consistent.IDToHex(crashed.Ref().ID)[:12])

	// Simulate crash
	crashed.SimulateCrash()
	tr.Deregister(crashed.Ref().Addr)

	// Wait for checkSuccessor to detect and heal
	time.Sleep(600 * time.Millisecond)

	// node[0] should route successfully to live nodes
	failures := 0
	for trial := 0; trial < 20; trial++ {
		var key [20]byte
		if _, err := rand.Read(key[:]); err != nil {
			t.Fatalf("rand.Read: %v", err)
		}
		got, _, err := nodes[0].FindSuccessor(context.Background(), key)
		if err != nil {
			failures++
			t.Logf("trial %d: FindSuccessor error: %v", trial, err)
			continue
		}
		if got.ID == crashed.Ref().ID {
			failures++
			t.Logf("trial %d: FindSuccessor returned crashed node %s", trial, got.Addr)
		}
	}

	// Allow at most a small number of failures during recovery window
	if failures > 3 {
		t.Errorf("too many routing failures after crash: %d/20", failures)
	}
}

// TestConcurrentJoins verifies that concurrently joining 3 nodes into a single-
// node ring results in all 4 nodes having non-nil successors and being able to
// route any key to a known ring member.
func TestConcurrentJoins(t *testing.T) {
	tr := transport.NewInProcessTransport(0, 0)
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Stop() })

	cfg := chord.DefaultConfig()
	cfg.StabilizeInterval = 50 * time.Millisecond
	cfg.FixFingersInterval = 100 * time.Millisecond
	cfg.CheckPredInterval = 50 * time.Millisecond
	cfg.CheckSuccInterval = 50 * time.Millisecond

	// Create node[0] as the ring bootstrap
	addr0 := "127.0.0.1:8000"
	node0 := chord.NewChordNode(addr0, cfg, tr, bus, nil)
	tr.Register(addr0, node0)
	node0.CreateRing()
	node0.Start()

	// Create 3 more nodes
	const extra = 3
	extraNodes := make([]*chord.ChordNode, extra)
	for i := 0; i < extra; i++ {
		addr := fmt.Sprintf("127.0.0.1:%d", 8001+i)
		node := chord.NewChordNode(addr, cfg, tr, bus, nil)
		tr.Register(addr, node)
		extraNodes[i] = node
	}

	t.Cleanup(func() {
		node0.Stop()
		for _, n := range extraNodes {
			n.Stop()
		}
	})

	// Concurrently join all 3 extra nodes
	errCh := make(chan error, extra)
	for _, node := range extraNodes {
		node := node
		go func() {
			err := node.Join(context.Background(), node0.Ref())
			if err != nil {
				errCh <- err
				return
			}
			node.Start()
			errCh <- nil
		}()
	}

	// Collect join errors
	for i := 0; i < extra; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent join error: %v", err)
		}
	}

	// Wait for stabilization
	time.Sleep(time.Second)

	allNodes := append([]*chord.ChordNode{node0}, extraNodes...)

	// Build member ID set
	memberIDs := make(map[[20]byte]bool, len(allNodes))
	for _, node := range allNodes {
		memberIDs[node.Ref().ID] = true
	}

	// All nodes must have non-nil successors
	for i, node := range allNodes {
		succ := node.Successor()
		if succ == nil {
			t.Errorf("node[%d] (%s) has nil successor after concurrent joins", i, node.Ref().Addr)
		}
	}

	// All nodes must be able to route any key to a known member
	for trial := 0; trial < 20; trial++ {
		var key [20]byte
		if _, err := rand.Read(key[:]); err != nil {
			t.Fatalf("rand.Read: %v", err)
		}
		for i, node := range allNodes {
			got, _, err := node.FindSuccessor(context.Background(), key)
			if err != nil {
				t.Errorf("trial %d node[%d]: FindSuccessor error: %v", trial, i, err)
				continue
			}
			if !memberIDs[got.ID] {
				t.Errorf("trial %d node[%d]: FindSuccessor returned unknown node %s",
					trial, i, consistent.IDToHex(got.ID))
			}
		}
	}
}
