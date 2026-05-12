package replication

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sanskarpan/dht-system/dht-system/internal/consistent"
	"github.com/sanskarpan/dht-system/dht-system/internal/events"
	"github.com/sanskarpan/dht-system/dht-system/internal/store"
	"github.com/sanskarpan/dht-system/dht-system/internal/transport"
)

// ErrQuorumNotMet is returned when not enough replicas responded.
var ErrQuorumNotMet = errors.New("replication: quorum not met")

// Config holds quorum parameters.
type Config struct {
	N int // replication factor
	W int // write quorum
	R int // read quorum
}

// DefaultConfig returns sensible defaults (N=3, W=2, R=2).
func DefaultConfig() *Config {
	return &Config{N: 3, W: 2, R: 2}
}

// FindReplicasFn is a function that returns N replica nodes for a given key.
type FindReplicasFn func(key [20]byte) []transport.NodeRef

// QuorumManager handles quorum reads and writes.
type QuorumManager struct {
	Config       *Config
	Transport    transport.Transport
	FindReplicas FindReplicasFn
	Bus          events.EventEmitter
	Timeout      time.Duration // per-replica RPC timeout, default 2s
}

// NewQuorumManager creates a QuorumManager.
func NewQuorumManager(cfg *Config, t transport.Transport, findReplicas FindReplicasFn, bus events.EventEmitter) *QuorumManager {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &QuorumManager{
		Config:       cfg,
		Transport:    t,
		FindReplicas: findReplicas,
		Bus:          bus,
		Timeout:      2 * time.Second,
	}
}

// Write performs a quorum write. Fans out to N replicas and waits for W acks.
// Emits a replication_lag event recording the time from write start to all N acks.
func (q *QuorumManager) Write(ctx context.Context, key [20]byte, value []byte, writerID string) error {
	writeStart := time.Now()

	replicas := q.FindReplicas(key)
	if len(replicas) == 0 {
		return fmt.Errorf("replication.Write: no replicas found for key %s", consistent.IDToHex(key))
	}
	coordinator := replicas[0]
	baseClock := q.readMergedClock(ctx, coordinator, key, replicas)

	entry := &store.ValueEntry{
		Key:       key,
		Value:     value,
		Clock:     Increment(baseClock, writerID),
		Timestamp: writeStart,
		NodeID:    writerID,
	}

	var (
		acks int32
		wg   sync.WaitGroup
	)

	for _, r := range replicas {
		wg.Add(1)
		go func(replica transport.NodeRef) {
			defer wg.Done()
			ctx2, cancel := context.WithTimeout(ctx, q.Timeout)
			defer cancel()
			ctx2 = transport.WithSender(ctx2, coordinator)
			err := q.Transport.PutEntry(ctx2, replica, key, entry)
			if err == nil {
				atomic.AddInt32(&acks, 1)
			}
		}(r)
	}

	// Wait for all N replicas to complete (success or failure).
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	<-done
	lagMs := time.Since(writeStart).Milliseconds()
	acksReceived := int(atomic.LoadInt32(&acks))

	keyHex := consistent.IDToHex(key)
	success := acksReceived >= q.Config.W
	if q.Bus != nil {
		q.Bus.Publish(events.MakeEvent(events.EventWriteQuorum, events.WriteQuorumPayload{
			Key:          keyHex,
			W:            q.Config.W,
			N:            q.Config.N,
			AcksReceived: acksReceived,
			Success:      success,
			LagMs:        lagMs,
		}))

		// Emit a dedicated replication_lag event for the metrics panel.
		q.Bus.Publish(events.MakeEvent(events.EventReplicationLag, events.ReplicationLagPayload{
			Key:        keyHex,
			LagMs:      lagMs,
			N:          q.Config.N,
			Replicated: acksReceived,
		}))
	}

	if !success {
		return fmt.Errorf("%w: got %d/%d acks (need %d)", ErrQuorumNotMet, acksReceived, len(replicas), q.Config.W)
	}
	return nil
}

// Read performs a quorum read. Collects R responses and returns the causally latest value.
// Also triggers read-repair on stale replicas.
func (q *QuorumManager) Read(ctx context.Context, key [20]byte) (*store.ValueEntry, error) {
	replicas := q.FindReplicas(key)
	if len(replicas) == 0 {
		return nil, fmt.Errorf("replication.Read: no replicas found for key %s", consistent.IDToHex(key))
	}

	type replicaResult struct {
		entry *store.ValueEntry
		err   error
		node  transport.NodeRef
	}

	results := make(chan replicaResult, len(replicas))
	coordinator := replicas[0]
	for _, r := range replicas {
		go func(replica transport.NodeRef) {
			ctx2, cancel := context.WithTimeout(ctx, q.Timeout)
			defer cancel()
			ctx2 = transport.WithSender(ctx2, coordinator)
			entry, err := q.Transport.GetEntry(ctx2, replica, key)
			results <- replicaResult{entry: entry, err: err, node: replica}
		}(r)
	}

	var responses []*store.ValueEntry
	var respondingNodes []transport.NodeRef
	for range replicas {
		r := <-results
		if r.err == nil && r.entry != nil {
			responses = append(responses, r.entry)
			respondingNodes = append(respondingNodes, r.node)
		}
	}

	conflictDetected := IsConcurrent(responses)
	if q.Bus != nil {
		q.Bus.Publish(events.MakeEvent(events.EventReadQuorum, events.ReadQuorumPayload{
			Key:               consistent.IDToHex(key),
			R:                 q.Config.R,
			N:                 q.Config.N,
			ResponsesReceived: len(responses),
			ConflictDetected:  conflictDetected,
		}))
	}

	if len(responses) < q.Config.R {
		return nil, fmt.Errorf("%w: got %d/%d responses (need %d)", ErrQuorumNotMet, len(responses), len(replicas), q.Config.R)
	}

	winner := Resolve(responses)
	if conflictDetected && q.Bus != nil {
		versions := make([]events.ConflictVersionPayload, 0, len(responses))
		for _, response := range responses {
			versions = append(versions, events.ConflictVersionPayload{
				NodeID:    response.NodeID,
				Timestamp: response.Timestamp.Format(time.RFC3339Nano),
				Clock:     response.Clock,
			})
		}
		q.Bus.Publish(events.MakeEvent(events.EventConflict, events.ConflictDetectedPayload{
			Key:      consistent.IDToHex(key),
			NodeID:   winner.NodeID,
			Strategy: "vector_clock",
			Versions: versions,
		}))
	}

	// Read-repair: update stale replicas asynchronously
	go q.readRepair(ctx, coordinator, key, winner, responses, respondingNodes)

	return winner, nil
}

// readRepair updates replicas that have stale values.
func (q *QuorumManager) readRepair(ctx context.Context, coordinator transport.NodeRef, key [20]byte, winner *store.ValueEntry, responses []*store.ValueEntry, nodes []transport.NodeRef) {
	for i, resp := range responses {
		if Compare(VectorClock(resp.Clock), VectorClock(winner.Clock)) == Before {
			ctx2, cancel := context.WithTimeout(ctx, q.Timeout)
			ctx2 = transport.WithSender(ctx2, coordinator)
			_ = q.Transport.PutEntry(ctx2, nodes[i], key, winner)
			cancel()
		}
	}
}

func (q *QuorumManager) readMergedClock(ctx context.Context, coordinator transport.NodeRef, key [20]byte, replicas []transport.NodeRef) VectorClock {
	merged := VectorClock(nil)
	for _, replica := range replicas {
		ctx2, cancel := context.WithTimeout(ctx, q.Timeout)
		ctx2 = transport.WithSender(ctx2, coordinator)
		entry, err := q.Transport.GetEntry(ctx2, replica, key)
		cancel()
		if err != nil || entry == nil {
			continue
		}
		merged = Merge(merged, VectorClock(entry.Clock))
	}
	return merged
}
