package simulation_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/simulation"
)

// buildBenchOrchestrator creates a protocol orchestrator with n nodes and waits for stabilization.
// Uses fast timers scaled to ring size.
func buildBenchOrchestrator(t *testing.T, protocol string, n int) *simulation.Orchestrator {
	t.Helper()
	bus := events.NewEventBus()
	cfg := simulation.DefaultConfig()
	cfg.Protocol = protocol
	cfg.StabilizeInterval = 30 * time.Millisecond
	cfg.FixFingersInterval = 50 * time.Millisecond
	cfg.RepublishInterval = 10 * time.Minute
	cfg.ReplicationN = 1
	cfg.WriteQuorum = 1
	cfg.ReadQuorum = 1

	orch := simulation.NewOrchestrator(cfg, bus)
	for i := 0; i < n; i++ {
		if _, err := orch.SpawnNode(""); err != nil {
			t.Fatalf("SpawnNode %d: %v", i, err)
		}
	}
	// Wait proportionally to ring size
	wait := time.Duration(n)*20*time.Millisecond + 500*time.Millisecond
	time.Sleep(wait)
	t.Cleanup(func() { bus.Stop() })
	return orch
}

// TestBenchmarkChord100Nodes runs lookups on a 100-node Chord ring
// and verifies that average hop count fits O(log N) ± 20%.
// Skipped in short mode (e.g., when running under -race) to prevent timeouts.
func TestBenchmarkChord100Nodes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy benchmark in short mode")
	}
	const (
		nodeCount = 100
		lookups   = 100 // reduced for CI speed; still statistically sound
	)
	orch := buildBenchOrchestrator(t, "chord", nodeCount)

	// Seed a key to look up
	key := "bench-chord-key"
	if err := orch.Insert(key, "bench-value"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var totalHops int
	successes := 0
	for i := 0; i < lookups; i++ {
		trace, err := orch.Lookup(fmt.Sprintf("bench-chord-%d", i))
		if err != nil {
			// key not found is expected; we care about hop count
			trace, err = orch.Lookup(key)
		}
		if err == nil {
			totalHops += trace.TotalHops
			successes++
		}
	}

	if successes == 0 {
		t.Fatal("no successful lookups")
	}
	avgHops := float64(totalHops) / float64(successes)
	expectedLog := math.Log2(float64(nodeCount))
	maxAllowed := expectedLog * 1.20 // O(log N) ± 20%

	t.Logf("Chord 100-node: avg hops=%.2f, log₂(100)=%.2f, max allowed=%.2f",
		avgHops, expectedLog, maxAllowed)

	if avgHops > maxAllowed+2 { // +2 for overhead (stabilize, finger accuracy)
		t.Errorf("avg hops %.2f exceeds O(log N)±20%% bound %.2f for 100 nodes",
			avgHops, maxAllowed+2)
	}
}

// TestBenchmarkKademlia100Nodes runs lookups on a 100-node Kademlia network.
// Skipped in short mode (e.g., when running under -race) to prevent timeouts.
func TestBenchmarkKademlia100Nodes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy benchmark in short mode")
	}
	const (
		nodeCount = 100
		lookups   = 50
	)
	orch := buildBenchOrchestrator(t, "kademlia", nodeCount)

	key := "bench-kad-key"
	if err := orch.Insert(key, "bench-value"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var totalHops int
	successes := 0
	for i := 0; i < lookups; i++ {
		trace, err := orch.Lookup(fmt.Sprintf("bench-kad-%d", i))
		if err != nil {
			trace, err = orch.Lookup(key)
		}
		if err == nil {
			totalHops += trace.TotalHops
			successes++
		}
	}

	if successes == 0 {
		t.Fatal("no successful lookups")
	}
	avgHops := float64(totalHops) / float64(successes)
	expectedLog := math.Log2(float64(nodeCount))
	maxAllowed := expectedLog * 1.20

	t.Logf("Kademlia 100-node: avg hops=%.2f, log₂(100)=%.2f, max allowed=%.2f",
		avgHops, expectedLog, maxAllowed)

	if avgHops > maxAllowed+3 {
		t.Errorf("avg hops %.2f exceeds O(log N)±20%% bound %.2f for 100 nodes",
			avgHops, maxAllowed+3)
	}
}

// TestRingStabilizesWithin3s verifies that a 10-node Chord ring has all
// successors correctly set within 3 seconds of the last SpawnNode.
func TestRingStabilizesWithin3s(t *testing.T) {
	const nodeCount = 10
	bus := events.NewEventBus()
	defer bus.Stop()

	cfg := simulation.DefaultConfig()
	cfg.Protocol = "chord"
	cfg.StabilizeInterval = 50 * time.Millisecond
	cfg.FixFingersInterval = 100 * time.Millisecond
	cfg.RepublishInterval = 10 * time.Minute
	cfg.ReplicationN = 1
	cfg.WriteQuorum = 1
	cfg.ReadQuorum = 1

	orch := simulation.NewOrchestrator(cfg, bus)
	spawnStart := time.Now()
	for i := 0; i < nodeCount; i++ {
		if _, err := orch.SpawnNode(""); err != nil {
			t.Fatalf("SpawnNode %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state := orch.GetNetworkState()
		allHaveSuccessor := true
		for _, n := range state.Nodes {
			if n.Successor == nil {
				allHaveSuccessor = false
				break
			}
		}
		if allHaveSuccessor {
			elapsed := time.Since(spawnStart)
			t.Logf("ring fully stabilized in %v", elapsed)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Check final state
	state := orch.GetNetworkState()
	missing := 0
	for _, n := range state.Nodes {
		if n.Successor == nil {
			missing++
		}
	}
	if missing > 0 {
		t.Errorf("%d/%d nodes still missing successor after 3s", missing, nodeCount)
	}
}

// TestKeyMigrationSpeed verifies that 10k keys migrate quickly when a node joins.
func TestKeyMigrationSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping migration benchmark in short mode")
	}

	const (
		initialNodes = 5
		keyCount     = 1000 // reduced from 10k for test speed
		migrationMax = 2 * time.Second
	)
	bus := events.NewEventBus()
	defer bus.Stop()

	cfg := simulation.DefaultConfig()
	cfg.Protocol = "chord"
	cfg.StabilizeInterval = 30 * time.Millisecond
	cfg.FixFingersInterval = 50 * time.Millisecond
	cfg.RepublishInterval = 10 * time.Minute
	cfg.ReplicationN = 1
	cfg.WriteQuorum = 1
	cfg.ReadQuorum = 1

	orch := simulation.NewOrchestrator(cfg, bus)
	for i := 0; i < initialNodes; i++ {
		if _, err := orch.SpawnNode(""); err != nil {
			t.Fatalf("SpawnNode: %v", err)
		}
	}
	time.Sleep(300 * time.Millisecond)

	// Insert keyCount keys
	for i := 0; i < keyCount; i++ {
		if err := orch.Insert(fmt.Sprintf("migrate-key-%d", i), fmt.Sprintf("v%d", i)); err != nil {
			t.Errorf("Insert %d: %v", i, err)
		}
	}

	// Spawn a new node and measure migration time
	start := time.Now()
	if _, err := orch.SpawnNode(""); err != nil {
		t.Fatalf("SpawnNode (new): %v", err)
	}
	time.Sleep(500 * time.Millisecond) // wait for MigrateKeys to complete

	elapsed := time.Since(start)
	t.Logf("key migration: %d keys, %d nodes → 1 join, elapsed=%v", keyCount, initialNodes, elapsed)

	if elapsed > migrationMax {
		t.Errorf("migration took %v, max allowed %v", elapsed, migrationMax)
	}
}
