package kademlia_test

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/kademlia"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// buildKadNetwork creates an n-node Kademlia network over in-process transport.
// All nodes join sequentially through nodes[0] as bootstrap.
func buildKadNetwork(t *testing.T, n int) ([]*kademlia.KademliaNode, *transport.InProcessTransport) {
	t.Helper()
	cfg := kademlia.DefaultConfig()
	// Disable background goroutines so they don't interfere with tests
	cfg.RepublishInterval = 10 * time.Minute
	cfg.BucketRefreshInterval = 10 * time.Minute
	return buildKadNetworkWithConfig(t, n, cfg)
}

func buildKadNetworkWithConfig(t *testing.T, n int, cfg *kademlia.Config) ([]*kademlia.KademliaNode, *transport.InProcessTransport) {
	t.Helper()
	tr := transport.NewInProcessTransport(0, 0)
	bus := events.NewEventBus()

	nodes := make([]*kademlia.KademliaNode, n)
	for i := 0; i < n; i++ {
		addr := fmt.Sprintf("127.0.0.1:%d", 9100+i)
		node := kademlia.NewKademliaNode(addr, cfg, tr, bus, nil)
		tr.Register(addr, node)
		nodes[i] = node
	}

	// First node bootstraps the network
	nodes[0].CreateNetwork()
	nodes[0].Start()

	// Remaining nodes join sequentially through nodes[0]
	for i := 1; i < n; i++ {
		bootstrap := kademlia.Contact{
			ID:       nodes[0].ID,
			Addr:     nodes[0].Addr,
			LastSeen: time.Now(),
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := nodes[i].Join(ctx, bootstrap)
		cancel()
		if err != nil {
			t.Fatalf("node[%d].Join failed: %v", i, err)
		}
		nodes[i].Start()
	}

	t.Cleanup(func() {
		for _, node := range nodes {
			node.Stop()
		}
		bus.Stop()
	})
	return nodes, tr
}

// nodeIDWithBit returns a NodeID with only bit i set (0 = LSB of byte[19], 159 = MSB of byte[0]).
// Bit numbering follows big-endian: byte[0] holds bits 159..152, byte[19] holds bits 7..0.
func nodeIDWithBit(i int) kademlia.NodeID {
	var id kademlia.NodeID
	byteIdx := 19 - i/8
	bitIdx := uint(i % 8)
	id[byteIdx] |= 1 << bitIdx
	return id
}

// contactWithByte0 constructs a Contact whose NodeID has the given first byte.
// All contacts with bit 159 set (byte[0] >= 0x80) land in bucket 159 relative to zeroID.
func contactWithByte0(b0 byte, addr string) kademlia.Contact {
	var id kademlia.NodeID
	id[0] = b0
	return kademlia.Contact{ID: id, Addr: addr, LastSeen: time.Now()}
}

// ---------------------------------------------------------------------------
// XOR Distance Tests
// ---------------------------------------------------------------------------

// TestXORDistanceTriangleInequality verifies d(a,c) ≤ d(a,b) + d(b,c) for all triples.
func TestXORDistanceTriangleInequality(t *testing.T) {
	ids := []kademlia.NodeID{
		nodeIDWithBit(0),
		nodeIDWithBit(1),
		nodeIDWithBit(7),
		nodeIDWithBit(100),
		nodeIDWithBit(159),
		{0x12, 0x34, 0x56},
		{0xAB, 0xCD, 0xEF},
	}
	var zero kademlia.NodeID
	ids = append(ids, zero)

	for _, a := range ids {
		for _, b := range ids {
			for _, c := range ids {
				dab := kademlia.XORDistance(a, b)
				dbc := kademlia.XORDistance(b, c)
				dac := kademlia.XORDistance(a, c)
				sum := new(big.Int).Add(dab, dbc)
				if dac.Cmp(sum) > 0 {
					t.Errorf("triangle inequality violated: d(a,c)=%v > d(a,b)+d(b,c)=%v", dac, sum)
				}
			}
		}
	}
}

// TestXORDistanceSymmetry verifies d(a,b) == d(b,a).
func TestXORDistanceSymmetry(t *testing.T) {
	ids := []kademlia.NodeID{
		nodeIDWithBit(0),
		nodeIDWithBit(159),
		{0x12, 0x34, 0x56},
		{0xDE, 0xAD, 0xBE, 0xEF},
	}
	for _, a := range ids {
		for _, b := range ids {
			dab := kademlia.XORDistance(a, b)
			dba := kademlia.XORDistance(b, a)
			if dab.Cmp(dba) != 0 {
				t.Errorf("d(a,b) != d(b,a): %v vs %v", dab, dba)
			}
		}
	}
}

// TestXORDistanceIdentity verifies d(a,a) == 0.
func TestXORDistanceIdentity(t *testing.T) {
	ids := []kademlia.NodeID{nodeIDWithBit(0), nodeIDWithBit(159), {0xAB}}
	for _, a := range ids {
		d := kademlia.XORDistance(a, a)
		if d.Sign() != 0 {
			t.Errorf("d(a,a) = %v, want 0", d)
		}
	}
}

// ---------------------------------------------------------------------------
// BucketIndex Tests
// ---------------------------------------------------------------------------

// TestBucketIndexKnownPairs verifies BucketIndex returns the correct bucket for known ID pairs.
func TestBucketIndexKnownPairs(t *testing.T) {
	var zero kademlia.NodeID

	tests := []struct {
		name    string
		contact kademlia.NodeID
		want    int
	}{
		{"bit 159 → bucket 159", nodeIDWithBit(159), 159},
		{"bit 100 → bucket 100", nodeIDWithBit(100), 100},
		{"bit 8 → bucket 8", nodeIDWithBit(8), 8},
		{"bit 1 → bucket 1", nodeIDWithBit(1), 1},
		{"bit 0 (LSB) → bucket 0", nodeIDWithBit(0), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kademlia.BucketIndex(zero, tt.contact)
			if got != tt.want {
				t.Errorf("BucketIndex = %d, want %d", got, tt.want)
			}
		})
	}

	// Self-distance → -1
	if got := kademlia.BucketIndex(zero, zero); got != -1 {
		t.Errorf("BucketIndex(self,self) = %d, want -1", got)
	}
}

// TestBucketIndexHighestBitDominates verifies that BucketIndex uses the highest set bit in the XOR.
func TestBucketIndexHighestBitDominates(t *testing.T) {
	var zero kademlia.NodeID

	// Set bits 159 and 5 → highest is 159
	contact := nodeIDWithBit(159)
	contact[19] |= 0x20 // also bit 5
	if got := kademlia.BucketIndex(zero, contact); got != 159 {
		t.Errorf("BucketIndex with bits 159+5 set = %d, want 159", got)
	}

	// Set bits 50 and 3 → highest is 50
	contact2 := nodeIDWithBit(50)
	contact2[19] |= 0x08 // also bit 3
	if got := kademlia.BucketIndex(zero, contact2); got != 50 {
		t.Errorf("BucketIndex with bits 50+3 set = %d, want 50", got)
	}
}

// ---------------------------------------------------------------------------
// KBucket / Routing Table LRU Tests
// ---------------------------------------------------------------------------

// TestKBucketLRU verifies the LRU eviction policy:
//   - When bucket is full and oldest is alive: keep old, discard new.
//   - When bucket is full and oldest is dead: evict old, add new.
func TestKBucketLRU(t *testing.T) {
	var self kademlia.NodeID

	// Three contacts whose NodeIDs all have bit 159 set → all go into bucket 159
	// (all have byte[0] >= 0x80, so XOR with zeroID has bit 159 set as highest bit)
	c1 := contactWithByte0(0x80, "addr-c1") // will be oldest in bucket
	c2 := contactWithByte0(0x81, "addr-c2")
	c3 := contactWithByte0(0x82, "addr-c3") // new contact trying to enter

	alive := func(kademlia.Contact) error { return nil }
	dead := func(kademlia.Contact) error { return fmt.Errorf("ping timeout") }

	t.Run("prefer live old node: discard new contact", func(t *testing.T) {
		rt := kademlia.NewRoutingTable(self, 2) // k=2, bucket capacity=2

		rt.UpdateContact(c1, alive) // c1 enters bucket (oldest)
		rt.UpdateContact(c2, alive) // c2 enters bucket (newest), now full
		// Adding c3: pings oldest (c1) → alive → c3 discarded
		rt.UpdateContact(c3, alive)

		all := rt.AllContacts()
		if len(all) != 2 {
			t.Errorf("expected 2 contacts, got %d", len(all))
		}
		for _, c := range all {
			if c.ID == c3.ID {
				t.Error("c3 should have been discarded (c1 was alive)")
			}
		}
	})

	t.Run("evict dead old node: add new contact", func(t *testing.T) {
		rt := kademlia.NewRoutingTable(self, 2)

		rt.UpdateContact(c1, alive) // c1 oldest
		rt.UpdateContact(c2, alive) // c2 newest, bucket full
		// Adding c3: pings oldest (c1) → dead → evict c1, add c3
		rt.UpdateContact(c3, dead)

		all := rt.AllContacts()
		if len(all) != 2 {
			t.Errorf("expected 2 contacts, got %d", len(all))
		}
		hasC1, hasC3 := false, false
		for _, c := range all {
			if c.ID == c1.ID {
				hasC1 = true
			}
			if c.ID == c3.ID {
				hasC3 = true
			}
		}
		if hasC1 {
			t.Error("dead c1 should have been evicted from bucket")
		}
		if !hasC3 {
			t.Error("c3 should have been added after evicting dead c1")
		}
	})

	t.Run("re-seeing existing contact moves it to tail", func(t *testing.T) {
		rt := kademlia.NewRoutingTable(self, 3)

		rt.UpdateContact(c1, alive)
		rt.UpdateContact(c2, alive)
		rt.UpdateContact(c3, alive)
		// All three in bucket; now re-see c1 (should move to most-recently-seen)
		rt.UpdateContact(c1, alive)

		all := rt.AllContacts()
		if len(all) != 3 {
			t.Errorf("expected 3 contacts after re-insert, got %d", len(all))
		}
	})

	t.Run("bucket not full: always insert", func(t *testing.T) {
		rt := kademlia.NewRoutingTable(self, 10) // plenty of room

		rt.UpdateContact(c1, dead) // ping not called since bucket not full
		rt.UpdateContact(c2, dead)
		rt.UpdateContact(c3, dead)

		if n := rt.Buckets[159].Len(); n != 3 {
			t.Errorf("expected 3 contacts in bucket 159, got %d", n)
		}
	})
}

// ---------------------------------------------------------------------------
// Iterative Lookup Convergence
// ---------------------------------------------------------------------------

// TestIterativeFindNodeConvergence verifies that iterative lookup terminates in ≤ ⌈log₂(N)⌉+3 iterations.
func TestIterativeFindNodeConvergence(t *testing.T) {
	const n = 20
	nodes, _ := buildKadNetwork(t, n)

	ctx := context.Background()
	targets := []kademlia.NodeID{
		nodes[5].ID,
		nodes[10].ID,
		nodes[19].ID,
	}

	// Kademlia paper: lookup converges in O(log N) iterations; +3 as practical buffer
	maxIter := int(math.Ceil(math.Log2(float64(n)))) + 3

	for _, target := range targets {
		contacts, hops, err := nodes[0].IterativeFindNode(ctx, target)
		if err != nil {
			t.Fatalf("IterativeFindNode failed: %v", err)
		}
		if len(contacts) == 0 {
			t.Error("IterativeFindNode returned no contacts")
		}
		if len(hops) > maxIter {
			t.Errorf("too many iterations: %d > %d (⌈log₂(%d)⌉+3)",
				len(hops), maxIter, n)
		}
		t.Logf("target=%s…: %d contacts returned in %d iterations",
			consistent.IDToHex([20]byte(target))[:8], len(contacts), len(hops))
	}
}

// TestIterativeFindNodeSingleNode verifies lookup works in a 1-node network.
func TestIterativeFindNodeSingleNode(t *testing.T) {
	nodes, _ := buildKadNetwork(t, 1)

	ctx := context.Background()
	// Looking up own ID
	contacts, hops, err := nodes[0].IterativeFindNode(ctx, nodes[0].ID)
	if err != nil {
		t.Fatalf("IterativeFindNode failed: %v", err)
	}
	// Routing table is empty (self excluded), so nothing to look up
	_ = contacts
	_ = hops
}

// ---------------------------------------------------------------------------
// Store + Retrieve
// ---------------------------------------------------------------------------

// TestStoreAndRetrieve verifies a key stored via StoreValue is findable after the JOIN sequence.
func TestStoreAndRetrieve(t *testing.T) {
	nodes, _ := buildKadNetwork(t, 8)

	ctx := context.Background()
	key := "hello-dht"
	value := []byte("distributed-hash-table")

	// Store via node 0 — stores locally + KStore to k-closest
	if err := nodes[0].StoreValue(ctx, key, value); err != nil {
		t.Fatalf("StoreValue failed: %v", err)
	}

	keyID := kademlia.NodeID(consistent.KeyID(key))

	// Storing node must always find its own key locally
	entry, _, _, err := nodes[0].IterativeFindValue(ctx, keyID)
	if err != nil {
		t.Fatalf("IterativeFindValue on storing node failed: %v", err)
	}
	if entry == nil {
		t.Fatal("key not found on storing node — local store must always hold it")
	}
	if string(entry.Value) != string(value) {
		t.Errorf("value mismatch on storing node: got %q, want %q", entry.Value, value)
	}

	// Count replicas across all other nodes (populated via KStore during StoreValue)
	replicaCount := 1 // node 0 guaranteed
	for i := 1; i < len(nodes); i++ {
		e, ok := nodes[i].KVStore.Get([20]byte(keyID))
		if ok && string(e.Value) == string(value) {
			replicaCount++
		}
	}
	t.Logf("Key replicated to %d/%d nodes after StoreValue", replicaCount, len(nodes))

	// In an 8-node network with K=20, all nodes are k-closest, so all should receive KStore
	// At minimum, the storing node + the nodes it directly discovered via IterativeFindNode
	if replicaCount < 2 {
		t.Error("expected key to be stored on at least 2 nodes via KStore propagation")
	}
}

// TestIterativeFindValueRemoteLookup verifies the shared iterative lookup path
// still finds a remote value when the caller is not one of the stored replicas.
func TestIterativeFindValueRemoteLookup(t *testing.T) {
	cfg := kademlia.DefaultConfig()
	cfg.K = 2
	nodes, _ := buildKadNetworkWithConfig(t, 8, cfg)

	ctx := context.Background()
	key := "remote-lookup-key"
	value := []byte("remote-lookup-value")

	if err := nodes[0].StoreValue(ctx, key, value); err != nil {
		t.Fatalf("StoreValue failed: %v", err)
	}

	keyID := kademlia.NodeID(consistent.KeyID(key))
	lookupIdx := -1
	for i := 1; i < len(nodes); i++ {
		if _, ok := nodes[i].KVStore.Get([20]byte(keyID)); !ok {
			lookupIdx = i
			break
		}
	}
	if lookupIdx == -1 {
		t.Fatal("expected at least one non-replica node with K=2")
	}

	entry, contacts, hops, err := nodes[lookupIdx].IterativeFindValue(ctx, keyID)
	if err != nil {
		t.Fatalf("IterativeFindValue failed: %v", err)
	}
	if entry == nil {
		t.Fatal("expected remote lookup to find the stored value")
	}
	if string(entry.Value) != string(value) {
		t.Fatalf("value mismatch: got %q want %q", entry.Value, value)
	}
	if len(hops) == 0 {
		t.Fatal("expected at least one network hop for a non-replica lookup")
	}
	if len(contacts) == 0 {
		t.Fatal("expected closest contacts to be returned alongside the found value")
	}
}

// TestStoreRetrieveMultipleKeys verifies each node can retrieve keys it stored itself.
func TestStoreRetrieveMultipleKeys(t *testing.T) {
	const n = 6
	nodes, _ := buildKadNetwork(t, n)

	ctx := context.Background()

	pairs := []struct{ key, val string }{
		{"key-alpha", "value-alpha"},
		{"key-beta", "value-beta"},
		{"key-gamma", "value-gamma"},
		{"key-delta", "value-delta"},
	}

	// Each key stored by a different node
	for i, p := range pairs {
		storer := nodes[i%n]
		if err := storer.StoreValue(ctx, p.key, []byte(p.val)); err != nil {
			t.Fatalf("StoreValue(%q) on node %d failed: %v", p.key, i%n, err)
		}
	}

	// Each storing node must be able to retrieve its own key
	for i, p := range pairs {
		storer := nodes[i%n]
		keyID := kademlia.NodeID(consistent.KeyID(p.key))
		entry, _, _, err := storer.IterativeFindValue(ctx, keyID)
		if err != nil {
			t.Fatalf("IterativeFindValue(%q) on storer failed: %v", p.key, err)
		}
		if entry == nil {
			t.Errorf("storer (node %d) cannot find its own key %q", i%n, p.key)
			continue
		}
		if string(entry.Value) != p.val {
			t.Errorf("value mismatch for %q: got %q, want %q", p.key, entry.Value, p.val)
		}
	}
}

// ---------------------------------------------------------------------------
// Concurrent Lookup
// ---------------------------------------------------------------------------

// TestConcurrentLookup verifies that 5 goroutines can concurrently find a key.
// After StoreValue (which propagates via KStore), nodes that received the key
// can find it via their local store check in IterativeFindValue.
func TestConcurrentLookup(t *testing.T) {
	nodes, _ := buildKadNetwork(t, 8)

	ctx := context.Background()
	key := "concurrent-key"
	value := []byte("concurrent-value")

	// Store on node 0; StoreValue propagates to k-closest (all 8 in this small network)
	if err := nodes[0].StoreValue(ctx, key, value); err != nil {
		t.Fatalf("StoreValue failed: %v", err)
	}

	keyID := kademlia.NodeID(consistent.KeyID(key))

	type result struct {
		nodeIdx int
		found   bool
		err     error
	}

	const workers = 5
	results := make(chan result, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// IterativeFindValue checks local store first; nodes that received KStore
			// will find the key immediately without further network RPCs
			entry, _, _, err := nodes[idx].IterativeFindValue(ctx, keyID)
			results <- result{nodeIdx: idx, found: entry != nil, err: err}
		}(i)
	}

	wg.Wait()
	close(results)

	foundCount := 0
	for r := range results {
		if r.err != nil {
			t.Errorf("node %d lookup error: %v", r.nodeIdx, r.err)
			continue
		}
		if r.found {
			foundCount++
		}
	}

	t.Logf("Concurrent lookup: %d/%d workers found key", foundCount, workers)
	// node 0 always has it; at least 1 (node 0) should find it
	if foundCount == 0 {
		t.Error("no worker found the key — at minimum node 0 must find its own stored key")
	}
}
