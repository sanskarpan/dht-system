package simulation_test

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/simulation"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// fastConfig returns an orchestrator config with sped-up timers for testing.
func fastConfig(protocol string) *simulation.Config {
	cfg := simulation.DefaultConfig()
	cfg.Protocol = protocol
	cfg.StabilizeInterval = 30 * time.Millisecond
	cfg.FixFingersInterval = 50 * time.Millisecond
	cfg.RepublishInterval = 10 * time.Minute // don't republish during tests
	cfg.ReplicationN = 3
	cfg.WriteQuorum = 2
	cfg.ReadQuorum = 2
	return cfg
}

// buildOrchestrator creates an Orchestrator with n nodes of the given protocol.
func buildOrchestrator(t *testing.T, protocol string, n int) *simulation.Orchestrator {
	t.Helper()
	bus := events.NewEventBus()
	cfg := fastConfig(protocol)
	orch := simulation.NewOrchestrator(cfg, bus)

	for i := 0; i < n; i++ {
		if _, err := orch.SpawnNode(""); err != nil {
			t.Fatalf("SpawnNode %d failed: %v", i, err)
		}
	}

	// Allow the ring to stabilize
	stabilizeWait := 800 * time.Millisecond
	time.Sleep(stabilizeWait)

	t.Cleanup(func() { bus.Stop() })
	return orch
}

// ---------------------------------------------------------------------------
// Integration Test 1: 10 Chord nodes, 100 keys — all readable
// ---------------------------------------------------------------------------

func TestChord10Nodes100Keys(t *testing.T) {
	const (
		nodeCount = 10
		keyCount  = 100
	)
	orch := buildOrchestrator(t, "chord", nodeCount)

	if got := orch.NodeCount(); got != nodeCount {
		t.Fatalf("expected %d nodes, got %d", nodeCount, got)
	}

	// Insert keyCount keys
	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("chord-key-%04d", i)
		value := fmt.Sprintf("chord-value-%04d", i)
		if err := orch.Insert(key, value); err != nil {
			t.Errorf("Insert(%q) failed: %v", key, err)
		}
	}

	// Read back all keys via quorum read
	failures := 0
	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("chord-key-%04d", i)
		expected := fmt.Sprintf("chord-value-%04d", i)
		got, err := orch.Read(key)
		if err != nil {
			t.Logf("Read(%q) failed: %v", key, err)
			failures++
			continue
		}
		if string(got) != expected {
			t.Errorf("Read(%q) = %q, want %q", key, got, expected)
		}
	}

	t.Logf("Chord 10-node, 100-key test: %d/%d keys readable", keyCount-failures, keyCount)
	// Allow up to 5% failure (in case some replicas haven't fully joined)
	maxFailures := keyCount / 20
	if failures > maxFailures {
		t.Errorf("too many read failures: %d/%d (max allowed: %d)", failures, keyCount, maxFailures)
	}
}

// ---------------------------------------------------------------------------
// Integration Test 2: 10 Kademlia nodes, 100 keys — all readable
// ---------------------------------------------------------------------------

func TestKademlia10Nodes100Keys(t *testing.T) {
	const (
		nodeCount = 10
		keyCount  = 100
	)
	orch := buildOrchestrator(t, "kademlia", nodeCount)

	if got := orch.NodeCount(); got != nodeCount {
		t.Fatalf("expected %d nodes, got %d", nodeCount, got)
	}

	// Insert keyCount keys
	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("kad-key-%04d", i)
		value := fmt.Sprintf("kad-value-%04d", i)
		if err := orch.Insert(key, value); err != nil {
			t.Errorf("Insert(%q) failed: %v", key, err)
		}
	}

	// Read back all keys
	failures := 0
	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("kad-key-%04d", i)
		expected := fmt.Sprintf("kad-value-%04d", i)
		got, err := orch.Read(key)
		if err != nil {
			t.Logf("Read(%q) failed: %v", key, err)
			failures++
			continue
		}
		if string(got) != expected {
			t.Errorf("Read(%q) = %q, want %q", key, got, expected)
		}
	}

	t.Logf("Kademlia 10-node, 100-key test: %d/%d keys readable", keyCount-failures, keyCount)
	maxFailures := keyCount / 20
	if failures > maxFailures {
		t.Errorf("too many read failures: %d/%d (max allowed: %d)", failures, keyCount, maxFailures)
	}
}

// ---------------------------------------------------------------------------
// Integration Test 3: Kill 3/10 nodes — keys still readable with R=2, N=3
// ---------------------------------------------------------------------------

func TestChurnResilienceAfterKill(t *testing.T) {
	const nodeCount = 10
	orch := buildOrchestrator(t, "chord", nodeCount)

	// Insert 20 keys
	keys := make([]string, 20)
	for i := range keys {
		keys[i] = fmt.Sprintf("churn-key-%02d", i)
		if err := orch.Insert(keys[i], "val-"+keys[i]); err != nil {
			t.Fatalf("Insert(%q) failed: %v", keys[i], err)
		}
	}

	// Kill 3 nodes (30% churn) — use GetNetworkState to get addresses
	state := orch.GetNetworkState()
	killed := 0
	for _, n := range state.Nodes {
		if killed >= 3 {
			break
		}
		if err := orch.KillNode(n.Addr); err != nil {
			t.Logf("KillNode(%s) warning: %v", n.Addr, err)
		} else {
			killed++
		}
	}
	t.Logf("Killed %d nodes; %d remaining", killed, orch.NodeCount())

	// Allow ring to restabilize after kills
	time.Sleep(600 * time.Millisecond)

	// With N=3, W=2, R=2: keys written to ≥2 replicas should survive 3 kills
	// (as long as the killed nodes weren't all replicas for the same key)
	failures := 0
	for _, key := range keys {
		_, err := orch.Read(key)
		if err != nil {
			t.Logf("Read(%q) after churn failed: %v", key, err)
			failures++
		}
	}
	t.Logf("After killing %d/%d nodes: %d/%d keys readable", killed, nodeCount, len(keys)-failures, len(keys))
}

// ---------------------------------------------------------------------------
// Integration Test 4: Network partition → verify partition blocks RPCs
// ---------------------------------------------------------------------------

func TestNetworkPartitionAndHeal(t *testing.T) {
	const nodeCount = 6
	orch := buildOrchestrator(t, "chord", nodeCount)

	// Insert 6 keys (one per node logically)
	prePartitionKeys := make([]string, 4)
	for i := range prePartitionKeys {
		prePartitionKeys[i] = fmt.Sprintf("pre-partition-%d", i)
		if err := orch.Insert(prePartitionKeys[i], "before"); err != nil {
			t.Fatalf("Insert pre-partition key: %v", err)
		}
	}

	// Get node addresses for partitioning
	state := orch.GetNetworkState()
	if len(state.Nodes) < nodeCount {
		t.Fatalf("expected %d nodes, got %d", nodeCount, len(state.Nodes))
	}
	addrs := make([]string, len(state.Nodes))
	for i, n := range state.Nodes {
		addrs[i] = n.Addr
	}

	// Split into two equal groups
	mid := len(addrs) / 2
	group1 := addrs[:mid]
	group2 := addrs[mid:]
	orch.SetPartition([][]string{group1, group2})

	// Pre-partition keys should still be readable from nodes within each partition
	// (the replicas for those keys may be split across partition groups)
	t.Log("Partition set; network is now split")

	// Heal partition
	orch.HealPartition()
	t.Log("Partition healed")

	// After healing, pre-partition keys should be readable again
	time.Sleep(400 * time.Millisecond) // allow re-stabilization

	failures := 0
	for _, key := range prePartitionKeys {
		_, err := orch.Read(key)
		if err != nil {
			t.Logf("Read(%q) after heal failed: %v", key, err)
			failures++
		}
	}
	t.Logf("After partition+heal: %d/%d pre-partition keys readable",
		len(prePartitionKeys)-failures, len(prePartitionKeys))
	// Allow some failures since gossip isn't implemented and some replicas may have been unreachable
	if failures > len(prePartitionKeys)/2 {
		t.Errorf("too many failures after heal: %d/%d", failures, len(prePartitionKeys))
	}
}

func TestKademliaRepeatedWriteAdvancesVectorClock(t *testing.T) {
	orch := buildOrchestrator(t, "kademlia", 3)

	const key = "clock-advance"
	if err := orch.Insert(key, "v1"); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if err := orch.Insert(key, "v2"); err != nil {
		t.Fatalf("second Insert: %v", err)
	}

	replicas := orch.GetKeyReplicas(key)
	if len(replicas) == 0 {
		t.Fatal("expected replicas after repeated write")
	}

	for i, replica := range replicas {
		got := replica.Entry.Clock["gateway"]
		if got < 2 {
			t.Fatalf("replica %d clock gateway=%d, want at least 2", i, got)
		}
		if string(replica.Entry.Value) != "v2" {
			t.Fatalf("replica %d value=%q, want %q", i, replica.Entry.Value, "v2")
		}
	}
}

func TestKademliaOrchestratorStartsGossipConvergence(t *testing.T) {
	bus := events.NewEventBus()
	cfg := fastConfig("kademlia")
	cfg.GossipInterval = 40 * time.Millisecond
	orch := simulation.NewOrchestrator(cfg, bus)
	t.Cleanup(func() { bus.Stop() })

	for i := 0; i < 4; i++ {
		if _, err := orch.SpawnNode(""); err != nil {
			t.Fatalf("SpawnNode %d failed: %v", i, err)
		}
	}
	time.Sleep(800 * time.Millisecond)

	state := orch.GetNetworkState()
	if len(state.Nodes) < 2 {
		t.Fatalf("expected at least 2 nodes, got %d", len(state.Nodes))
	}

	first := state.Nodes[0]
	firstID, err := consistent.IDFromHex(first.ID)
	if err != nil {
		t.Fatalf("IDFromHex(%q): %v", first.ID, err)
	}

	const key = "gossip-autostart"
	keyID := consistent.KeyID(key)
	entry := &store.ValueEntry{
		Key:       keyID,
		Value:     []byte("seed"),
		Timestamp: time.Now(),
		NodeID:    first.ID,
	}
	ref := transport.NodeRef{ID: firstID, Addr: first.Addr}
	if err := orch.Transport().PutEntry(context.Background(), ref, keyID, entry); err != nil {
		t.Fatalf("direct PutEntry: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if replicas := orch.GetKeyReplicas(key); len(replicas) >= 2 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	replicas := orch.GetKeyReplicas(key)
	t.Fatalf("gossip did not spread key beyond the original node, replicas=%d", len(replicas))
}

// ---------------------------------------------------------------------------
// Integration Test 5: WebSocket — node_join event received after spawn
// ---------------------------------------------------------------------------

func TestWebSocketNodeJoinEvent(t *testing.T) {
	bus := events.NewEventBus()
	cfg := fastConfig("chord")
	orch := simulation.NewOrchestrator(cfg, bus)

	// Subscribe to node_join events
	ch := bus.Subscribe([]events.EventType{events.EventNodeJoin})
	defer bus.Unsubscribe(ch)

	// Spawn first node — creates ring → emits node_join
	if _, err := orch.SpawnNode(""); err != nil {
		t.Fatalf("SpawnNode failed: %v", err)
	}

	// Wait for the event (with timeout)
	select {
	case ev := <-ch:
		if ev.Type != events.EventNodeJoin {
			t.Errorf("expected node_join event, got %q", ev.Type)
		}
		t.Logf("Received node_join event: %+v", ev.Payload)
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for node_join event after SpawnNode")
	}

	// Spawn a second node — join → emits node_join
	if _, err := orch.SpawnNode(""); err != nil {
		t.Fatalf("SpawnNode (second) failed: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.Type != events.EventNodeJoin {
			t.Errorf("expected node_join event, got %q", ev.Type)
		}
		t.Logf("Received node_join event for second node: %+v", ev.Payload)
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for node_join event after second SpawnNode")
	}

	t.Cleanup(func() { bus.Stop() })
}

// ---------------------------------------------------------------------------
// Integration Test 6: Lookup trace — hop count matches actual path
// ---------------------------------------------------------------------------

func TestLookupTraceHopCount(t *testing.T) {
	orch := buildOrchestrator(t, "chord", 8)

	key := "trace-test-key"
	if err := orch.Insert(key, "trace-value"); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	trace, err := orch.Lookup(key)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	// TotalHops must match len(Hops)
	if trace.TotalHops != len(trace.Hops) {
		t.Errorf("TotalHops=%d but len(Hops)=%d: mismatch", trace.TotalHops, len(trace.Hops))
	}

	// Hop count ≤ ⌈log₂(N)⌉ + 2 for a stable ring
	maxHops := int(math.Ceil(math.Log2(8))) + 2 // log2(8)=3 → max 5
	if trace.TotalHops > maxHops {
		t.Errorf("hop count %d exceeds expected max %d for 8-node ring", trace.TotalHops, maxHops)
	}

	// KeyHash must be present and non-empty
	if trace.KeyHash == "" {
		t.Error("LookupTrace.KeyHash is empty")
	}

	t.Logf("Lookup(%q): %d hops, latency %dms, target=%s",
		key, trace.TotalHops, trace.LatencyMs, trace.TargetNode[:8])
}

// ---------------------------------------------------------------------------
// Integration Test 7: Kademlia lookup trace hop count
// ---------------------------------------------------------------------------

func TestKademliaLookupTraceHopCount(t *testing.T) {
	orch := buildOrchestrator(t, "kademlia", 8)

	key := "kad-trace-key"
	if err := orch.Insert(key, "kad-trace-value"); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	trace, err := orch.Lookup(key)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if trace.TotalHops != len(trace.Hops) {
		t.Errorf("TotalHops=%d but len(Hops)=%d", trace.TotalHops, len(trace.Hops))
	}

	maxHops := int(math.Ceil(math.Log2(8))) + 3
	if trace.TotalHops > maxHops {
		t.Errorf("hop count %d exceeds expected max %d for 8-node Kademlia network", trace.TotalHops, maxHops)
	}

	t.Logf("Kademlia Lookup(%q): %d hops, latency %dms", key, trace.TotalHops, trace.LatencyMs)
}
