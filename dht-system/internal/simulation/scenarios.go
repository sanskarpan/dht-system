package simulation

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/events"
)

// StepEvent is published on the bus to communicate scenario progress.
// It re-uses the generic "scenario_step" event type.
type StepEvent struct {
	Scenario string      `json:"scenario"`
	Step     int         `json:"step"`
	Total    int         `json:"total"`
	Message  string      `json:"message"`
	Data     interface{} `json:"data,omitempty"`
}

func (o *Orchestrator) publishStep(scenario string, step, total int, msg string, data interface{}) {
	o.bus.Publish(events.MakeEvent(events.EventScenarioStep, StepEvent{
		Scenario: scenario,
		Step:     step,
		Total:    total,
		Message:  msg,
		Data:     data,
	}))
}

// ScenarioBootstrap bootstraps a ring from 1 node up to targetNodes,
// emitting a step event after each addition.  It waits for stabilization
// between each spawn so the ring is consistent at each step.
func (o *Orchestrator) ScenarioBootstrap(ctx context.Context, targetNodes int) error {
	if targetNodes < 1 {
		targetNodes = 10
	}

	// Start fresh: kill all existing nodes
	state := o.GetNetworkState()
	for _, n := range state.Nodes {
		_ = o.KillNode(n.Addr)
	}
	time.Sleep(100 * time.Millisecond)

	for i := 0; i < targetNodes; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if _, err := o.SpawnNode(""); err != nil {
			return fmt.Errorf("ScenarioBootstrap: step %d: %w", i+1, err)
		}
		// Brief stabilization pause
		time.Sleep(o.cfg.StabilizeInterval * 2)

		o.publishStep("bootstrap", i+1, targetNodes,
			fmt.Sprintf("spawned node %d/%d, total=%d", i+1, targetNodes, o.NodeCount()),
			map[string]int{"nodeCount": o.NodeCount()})
	}
	return nil
}

// ScenarioChurn continuously adds and removes nodes for duration,
// emitting step events. It maintains roughly targetNodes nodes in the network.
func (o *Orchestrator) ScenarioChurn(ctx context.Context, duration time.Duration, targetNodes int) error {
	if targetNodes < 2 {
		targetNodes = 6
	}

	// Ensure we have targetNodes to start
	for o.NodeCount() < targetNodes {
		if _, err := o.SpawnNode(""); err != nil {
			return fmt.Errorf("ScenarioChurn: initial spawn: %w", err)
		}
	}
	time.Sleep(o.cfg.StabilizeInterval * 3)

	deadline := time.Now().Add(duration)
	step := 0
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		step++
		// Alternate: kill one random node, then spawn a new one
		state := o.GetNetworkState()
		if len(state.Nodes) > 1 {
			victim := state.Nodes[step%len(state.Nodes)].Addr
			_ = o.KillNode(victim)
			o.publishStep("churn", step, -1,
				fmt.Sprintf("killed node %s, remaining=%d", victim, o.NodeCount()),
				map[string]interface{}{"action": "kill", "addr": victim, "nodeCount": o.NodeCount()})
		}

		time.Sleep(o.cfg.StabilizeInterval * 2)

		if _, err := o.SpawnNode(""); err == nil {
			o.publishStep("churn", step, -1,
				fmt.Sprintf("spawned new node, total=%d", o.NodeCount()),
				map[string]interface{}{"action": "spawn", "nodeCount": o.NodeCount()})
		}

		time.Sleep(o.cfg.StabilizeInterval * 2)
	}
	return nil
}

// ScenarioPartition splits the network into two halves, waits for observationDur,
// then heals and measures convergence time (time until all keys readable).
func (o *Orchestrator) ScenarioPartition(ctx context.Context, keys []string, observationDur time.Duration) error {
	// Insert keys before partition
	for i, key := range keys {
		if err := o.Insert(key, fmt.Sprintf("partition-val-%d", i)); err != nil {
			return fmt.Errorf("ScenarioPartition: Insert(%s): %w", key, err)
		}
	}
	time.Sleep(o.cfg.StabilizeInterval * 2)

	// Split into two groups
	state := o.GetNetworkState()
	if len(state.Nodes) < 2 {
		return fmt.Errorf("ScenarioPartition: need at least 2 nodes, got %d", len(state.Nodes))
	}
	mid := len(state.Nodes) / 2
	group1 := make([]string, mid)
	group2 := make([]string, len(state.Nodes)-mid)
	for i, n := range state.Nodes {
		if i < mid {
			group1[i] = n.Addr
		} else {
			group2[i-mid] = n.Addr
		}
	}

	o.publishStep("partition", 1, 3, "setting network partition",
		map[string]interface{}{"group1Size": len(group1), "group2Size": len(group2)})
	o.SetPartition([][]string{group1, group2})

	select {
	case <-ctx.Done():
		o.HealPartition()
		return ctx.Err()
	case <-time.After(observationDur):
	}

	o.publishStep("partition", 2, 3, "healing partition", nil)
	healTime := time.Now()
	o.HealPartition()
	time.Sleep(o.cfg.StabilizeInterval * 3)

	// Measure convergence: how many keys are readable after heal
	readable := 0
	for _, key := range keys {
		if _, err := o.Read(key); err == nil {
			readable++
		}
	}
	convergenceMs := time.Since(healTime).Milliseconds()

	o.publishStep("partition", 3, 3, "convergence measured",
		map[string]interface{}{
			"keysReadable":  readable,
			"keysTotal":     len(keys),
			"convergenceMs": convergenceMs,
		})
	return nil
}

// ScenarioHotKey inserts keyCount keys distributed by SHA-1 and reports
// vnode load balance (keys-per-node distribution).
func (o *Orchestrator) ScenarioHotKey(ctx context.Context, keyCount int) error {
	if keyCount <= 0 {
		keyCount = 1000
	}

	for i := 0; i < keyCount; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		key := fmt.Sprintf("hotkey-%06d", i)
		_ = o.Insert(key, fmt.Sprintf("v%d", i))

		if (i+1)%100 == 0 {
			o.publishStep("hotkey", i+1, keyCount,
				fmt.Sprintf("inserted %d/%d keys", i+1, keyCount),
				map[string]int{"keysInserted": i + 1})
		}
	}

	// Report key counts per node
	state := o.GetNetworkState()
	counts := make(map[string]int, len(state.Nodes))
	for _, n := range state.Nodes {
		counts[n.Addr] = n.KeyCount
	}
	o.publishStep("hotkey", keyCount, keyCount, "load distribution measured", counts)
	return nil
}

// ScenarioBenchmark measures average lookup hop count as the network grows
// from minNodes to maxNodes in steps.
func (o *Orchestrator) ScenarioBenchmark(ctx context.Context, minNodes, maxNodes, lookupsPerStep int) error {
	if minNodes < 2 {
		minNodes = 10
	}
	if maxNodes < minNodes {
		maxNodes = minNodes * 5
	}
	if lookupsPerStep < 1 {
		lookupsPerStep = 20
	}

	// Kill all nodes, start fresh
	state := o.GetNetworkState()
	for _, n := range state.Nodes {
		_ = o.KillNode(n.Addr)
	}
	time.Sleep(200 * time.Millisecond)

	step := 0
	for n := minNodes; n <= maxNodes; n += max(1, (maxNodes-minNodes)/5) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		step++

		// Scale node count to n
		for o.NodeCount() < n {
			if _, err := o.SpawnNode(""); err != nil {
				return fmt.Errorf("ScenarioBenchmark: spawn: %w", err)
			}
		}
		time.Sleep(o.cfg.StabilizeInterval * 4)

		// Insert a key and measure hops
		key := fmt.Sprintf("bench-key-n%d", n)
		_ = o.Insert(key, "bench-value")

		var totalHops int
		successes := 0
		for i := 0; i < lookupsPerStep; i++ {
			trace, err := o.Lookup(fmt.Sprintf("bench-key-n%d-%d", n, i))
			if err != nil {
				// key doesn't exist yet, do a lookup anyway for hop count
				trace, err = o.Lookup(key)
			}
			if err == nil {
				totalHops += trace.TotalHops
				successes++
			}
		}

		avgHops := 0.0
		if successes > 0 {
			avgHops = float64(totalHops) / float64(successes)
		}
		expectedMax := math.Ceil(math.Log2(float64(n))) + 2

		o.publishStep("benchmark", step, -1,
			fmt.Sprintf("n=%d: avg hops=%.2f (expected≤%.0f)", n, avgHops, expectedMax),
			map[string]interface{}{
				"nodes":       n,
				"avgHops":     avgHops,
				"expectedMax": expectedMax,
				"successes":   successes,
			})

		if n == maxNodes {
			break
		}
	}
	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
