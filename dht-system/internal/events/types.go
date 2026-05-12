// Package events defines the DHT event system used for real-time observability.
//
// Events are published by DHT nodes and the orchestrator whenever significant
// state transitions occur (joins, lookups, quorum operations, gossip sync, etc.).
// The EventBus fans them out to all registered subscribers, including the
// WebSocket hub that streams them to the frontend.
package events

import (
	"encoding/json"
	"time"
)

type EventType string

const (
	EventNodeJoin       EventType = "node_join"
	EventNodeLeave      EventType = "node_leave"
	EventNodeCrash      EventType = "node_crash"
	EventStabilize      EventType = "stabilize"
	EventFixFingers     EventType = "fix_fingers"
	EventNotify         EventType = "notify"
	EventKeyMigrate     EventType = "key_migrate"
	EventLookupStart    EventType = "lookup_start"
	EventLookupHop      EventType = "lookup_hop"
	EventLookupComplete EventType = "lookup_complete"
	EventWriteQuorum    EventType = "write_quorum"
	EventReadQuorum     EventType = "read_quorum"
	EventVectorClock    EventType = "vector_clock"
	EventConflict       EventType = "conflict_detected"
	EventGossipSync     EventType = "gossip_sync"
	EventBucketUpdate   EventType = "bucket_update"
	EventRepublish      EventType = "republish"
	EventRingState      EventType = "ring_state"
	EventScenarioStep   EventType = "scenario_step"
	EventReplicationLag EventType = "replication_lag"
	EventAll            EventType = "*"
)

type Event struct {
	Type      EventType       `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// Specific payload structs for each event type

type NodeJoinPayload struct {
	NodeID      string `json:"nodeId"`
	Addr        string `json:"addr"`
	Successor   string `json:"successor,omitempty"`
	Predecessor string `json:"predecessor,omitempty"`
}

type NodeLeavePayload struct {
	NodeID          string `json:"nodeId"`
	KeysTransferred int    `json:"keysTransferred"`
}

type NodeCrashPayload struct {
	NodeID string `json:"nodeId"`
}

type StabilizePayload struct {
	NodeID      string `json:"nodeId"`
	Successor   string `json:"successor,omitempty"`
	Predecessor string `json:"predecessor,omitempty"`
}

type FixFingersPayload struct {
	NodeID     string `json:"nodeId"`
	Index      int    `json:"index"`
	FingerNode string `json:"fingerNode"`
}

type NotifyPayload struct {
	NodeID  string `json:"nodeId"`
	NewPred string `json:"newPredecessor"`
}

type KeyMigratePayload struct {
	FromNode string `json:"fromNode"`
	ToNode   string `json:"toNode"`
	KeyCount int    `json:"keyCount"`
}

type LookupStartPayload struct {
	Key      string `json:"key"`
	KeyHash  string `json:"keyHash"`
	FromNode string `json:"fromNode"`
}

type LookupHopPayload struct {
	FromNode  string `json:"fromNode"`
	ToNode    string `json:"toNode"`
	Mechanism string `json:"mechanism"`
	HopIndex  int    `json:"hopIndex"`
}

type LookupCompletePayload struct {
	Key        string `json:"key"`
	KeyHash    string `json:"keyHash"`
	TargetNode string `json:"targetNode"`
	TotalHops  int    `json:"totalHops"`
	LatencyMs  int64  `json:"latencyMs"`
}

type WriteQuorumPayload struct {
	Key          string `json:"key"`
	W            int    `json:"w"`
	N            int    `json:"n"`
	AcksReceived int    `json:"acksReceived"`
	Success      bool   `json:"success"`
	LagMs        int64  `json:"lagMs"` // time from write start to all N acks
}

// ReplicationLagPayload is emitted after a quorum write completes across all N replicas.
type ReplicationLagPayload struct {
	Key        string `json:"key"`
	LagMs      int64  `json:"lagMs"`
	N          int    `json:"n"`
	Replicated int    `json:"replicated"`
}

type ReadQuorumPayload struct {
	Key               string `json:"key"`
	R                 int    `json:"r"`
	N                 int    `json:"n"`
	ResponsesReceived int    `json:"responsesReceived"`
	ConflictDetected  bool   `json:"conflictDetected"`
}

type ConflictVersionPayload struct {
	NodeID    string            `json:"nodeId"`
	Timestamp string            `json:"timestamp"`
	Clock     map[string]uint64 `json:"clock"`
}

type ConflictDetectedPayload struct {
	Key      string                   `json:"key"`
	NodeID   string                   `json:"nodeId"`
	Strategy string                   `json:"strategy"`
	Versions []ConflictVersionPayload `json:"versions"`
}

type GossipSyncPayload struct {
	FromNode        string `json:"fromNode"`
	ToNode          string `json:"toNode"`
	KeysReconciled  int    `json:"keysReconciled"`
	DivergentBefore int    `json:"divergentBefore"`
	DivergentAfter  int    `json:"divergentAfter"`
}

type BucketUpdatePayload struct {
	NodeID      string `json:"nodeId"`
	BucketIndex int    `json:"bucketIndex"`
	ContactID   string `json:"contactId"`
	ContactAddr string `json:"contactAddr"`
}

type RepublishPayload struct {
	Key    string `json:"key"`
	NodeID string `json:"nodeId"`
}

// MakeEvent is a helper to create an Event with JSON-encoded payload.
// If marshaling fails, the payload is set to a JSON null to avoid silent data corruption.
func MakeEvent(t EventType, payload interface{}) Event {
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte("null")
	}
	return Event{
		Type:      t,
		Timestamp: time.Now(),
		Payload:   json.RawMessage(b),
	}
}
