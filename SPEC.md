# SPEC.md — Distributed Hash Table System
## Chord / Kademlia P2P DHT with Consistent Hashing, Replication & Eventual Consistency

> **Stack:** Go 1.22+ (backend) · React 18 + TypeScript (frontend)  
> **Protocols:** Chord (ring DHT) + Kademlia (XOR DHT)  
> **Version:** 1.0.0

---

## Table of Contents

1. [System Overview](#1-system-overview)
2. [Architecture](#2-architecture)
3. [Consistent Hashing Layer](#3-consistent-hashing-layer)
4. [Chord Protocol Specification](#4-chord-protocol-specification)
5. [Kademlia Protocol Specification](#5-kademlia-protocol-specification)
6. [Replication & Consistency Model](#6-replication--consistency-model)
7. [Transport & RPC Layer](#7-transport--rpc-layer)
8. [Gateway API Specification](#8-gateway-api-specification)
9. [Frontend Specification](#9-frontend-specification)
10. [Data Models & Schemas](#10-data-models--schemas)
11. [Configuration Reference](#11-configuration-reference)
12. [Error Handling & Fault Tolerance](#12-error-handling--fault-tolerance)
13. [Testing Strategy](#13-testing-strategy)
14. [Performance Targets](#14-performance-targets)
15. [Project File Structure](#15-project-file-structure)

---

## 1. System Overview

### 1.1 Purpose

A fully functional, visually interactive DHT runtime that implements **both Chord and Kademlia** protocols. Nodes run as real goroutines communicating over in-process gRPC transport (switchable to real TCP). The React frontend streams live events over WebSocket to animate ring topology, lookup routing, key replication, and eventual consistency convergence in real time.

### 1.2 Key Design Goals

| Goal | Approach |
|------|----------|
| Protocol fidelity | Implement algorithms exactly as described in original papers |
| Real concurrency | Goroutine-per-node, real channels, real timers |
| Live observability | Every operation emits structured events → WebSocket stream |
| Visual correctness | Frontend animations derived from actual node state, not simulation |
| Switchable protocols | Hot-swap Chord ↔ Kademlia at runtime without restarting |
| Configurable consistency | Quorum W/R/N tunable at runtime with visible effect |

### 1.3 Scope Boundaries

**In scope:**
- Full Chord stabilization loop, finger table, SuccessorList
- Full Kademlia k-buckets, α-concurrent iterative lookup, republication
- Consistent hashing with virtual nodes
- Quorum-based replication with vector clocks
- Anti-entropy Merkle gossip
- React frontend with D3 ring visualization, lookup tracer, consistency dashboard

**Out of scope (stretch):**
- Production TLS / mTLS
- Persistent storage (BadgerDB) — in-memory only for v1.0
- Cross-host deployment (in-process simulation only for v1.0)
- S/Kademlia proof-of-work node IDs

---

## 2. Architecture

### 2.1 High-Level Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    React + TypeScript Frontend               │
│  ┌──────────────┐ ┌───────────────┐ ┌────────────────────┐  │
│  │ Ring Canvas  │ │ Lookup Tracer │ │ Consistency Dash   │  │
│  │  (D3.js)    │ │               │ │ (Vector Clocks)    │  │
│  └──────┬───────┘ └───────┬───────┘ └──────────┬─────────┘  │
│         └─────────────────┼──────────────────────┘           │
│                    WebSocket + REST                           │
└─────────────────────────┬───────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                    Go Gateway Server                         │
│  ┌───────────────┐  ┌─────────────────┐  ┌───────────────┐  │
│  │  REST API     │  │  WebSocket Hub  │  │ Event Bus     │  │
│  │  (gin/chi)    │  │                 │  │ (chan Events)  │  │
│  └───────┬───────┘  └────────┬────────┘  └───────┬───────┘  │
│          └──────────────────┬┘───────────────────┘           │
│                    Node Orchestrator                          │
└──────────────────────────┬──────────────────────────────────┘
                           │  in-process gRPC / direct call
     ┌─────────────────────┼──────────────────────────┐
     │                     │                          │
┌────▼────┐           ┌────▼────┐               ┌────▼────┐
│ Node 1  │◄─────────►│ Node 2  │◄─────────────►│ Node N  │
│ Chord/  │  gRPC RPC │ Chord/  │   stabilize   │ Chord/  │
│ Kad     │           │ Kad     │               │ Kad     │
└─────────┘           └─────────┘               └─────────┘
```

### 2.2 Node Internal Architecture

```
┌──────────────────────────────────────────────────────┐
│                       DHT Node                        │
│                                                       │
│  ┌─────────────┐    ┌──────────────┐                 │
│  │ Protocol    │    │   KV Store   │                 │
│  │ Engine      │    │  (with TTL + │                 │
│  │ (Chord OR   │    │  VectorClock)│                 │
│  │  Kademlia)  │    └──────────────┘                 │
│  └──────┬──────┘                                     │
│         │                                            │
│  ┌──────▼──────────────────────────────────────┐    │
│  │           Background Goroutines              │    │
│  │  stabilize() │ fix_fingers() │ check_pred()  │    │
│  │  republish() │ anti_entropy()│ check_succ()  │    │
│  └─────────────────────────────────────────────┘    │
│                                                       │
│  ┌──────────────────────────────────────────────┐    │
│  │           RPC Handler (gRPC server)           │    │
│  │  FindSuccessor │ Notify │ GetPredecessor      │    │
│  │  Ping │ Store │ FindNode │ FindValue           │    │
│  └──────────────────────────────────────────────┘    │
│                                                       │
│  ┌──────────────────────────────────────────────┐    │
│  │           Event Emitter                       │    │
│  │  → gateway event bus on every state change   │    │
│  └──────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────┘
```

---

## 3. Consistent Hashing Layer

### 3.1 Hash Ring

- **Key space:** 2^m where `m = 160` (SHA-1) or `m = 256` (SHA-256, configurable)
- **Hash function:** `SHA-1(input)` → 20-byte array → interpreted as big-endian uint
- **Node ID:** `SHA-1(IP + ":" + Port)`
- **Key ID:** `SHA-1(key_string)`
- **Ring arithmetic:** All operations modulo 2^m

### 3.2 Interval Checks

```go
// IsInInterval checks if id ∈ (start, end) on the ring (exclusive both ends)
func IsInInterval(id, start, end [20]byte) bool

// IsInIntervalRightClosed checks id ∈ (start, end] (inclusive end)
func IsInIntervalRightClosed(id, start, end [20]byte) bool
```

### 3.3 Virtual Nodes

- Each physical node registers `V` virtual node IDs (default `V = 3`)
- Virtual node IDs: `SHA-1(IP:Port:vnode_index)` for `index = 0..V-1`
- All virtual nodes of a physical node share the same KV store
- Load balance metric: `std_dev(keys_per_physical_node) / mean(keys_per_physical_node)` — target < 0.2

### 3.4 Key Migration

On any topology change (join/leave), keys must migrate to maintain the invariant:

> A key `k` is stored on the node whose ID is `successor(k)` — the first node clockwise from `k`.

```
Migration Trigger:
  JOIN:  new node N takes keys in range (predecessor(N), N] from N.successor
  LEAVE: departing node D transfers all its keys to D.successor
  CRASH: D.successor inherits D's keys via replication (no explicit transfer)
```

---

## 4. Chord Protocol Specification

### 4.1 Data Structures

```go
// RemoteNode represents a reference to another node in the ring
type RemoteNode struct {
    ID   [20]byte
    Addr string // host:port
}

// ChordNode is the local node
type ChordNode struct {
    ID          [20]byte
    Addr        string
    Successor   *RemoteNode      // = finger[0]
    Predecessor *RemoteNode
    Fingers     [160]*RemoteNode // finger[i] = successor(n + 2^i mod 2^m)
    SuccList    []*RemoteNode    // len = ceil(log2(N)), default 8
    Store       *KVStore
    mu          sync.RWMutex
    
    // Background goroutine control
    stopCh      chan struct{}
    
    // Config
    Config      *ChordConfig
    
    // Event emission
    EventBus    EventEmitter
}
```

### 4.2 Finger Table

Entry `i` (0-indexed): `finger[i].node = successor(n + 2^i mod 2^m)`

```
finger[0] = immediate successor          distance 1
finger[1] = successor at distance 2      
finger[2] = successor at distance 4      
...
finger[i] = successor at distance 2^i
...
finger[159] = successor at distance 2^159  (half the ring away)
```

**Initialization on join:**
1. `finger[0] = find_successor(n.ID + 1)` via bootstrap node
2. For `i = 1..159`: if `n + 2^i` falls between `n` and `finger[i-1]`, reuse `finger[i-1]`; otherwise call `find_successor(n + 2^i)` — O(log²N) total

### 4.3 Core Algorithms

```
FIND_SUCCESSOR(id):
  if id ∈ (n, successor]:
    return successor
  else:
    n' = closest_preceding_node(id)
    return n'.find_successor(id)   // RPC

CLOSEST_PRECEDING_NODE(id):
  for i = 159 downto 0:
    if finger[i] ∈ (n, id):
      return finger[i]
  return n

STABILIZE():                        // runs every T_stabilize ms
  x = successor.get_predecessor()   // RPC
  if x ∈ (n, successor):
    successor = x
  successor.notify(n)               // RPC

NOTIFY(n'):
  if predecessor == nil OR n' ∈ (predecessor, n):
    predecessor = n'
    trigger key migration (n' takes keys ∈ (predecessor, n'])

FIX_FINGERS():                      // runs every T_fix_fingers ms
  i = random(0, 159)
  finger[i] = find_successor(n + 2^i)

CHECK_PREDECESSOR():                // runs every T_check_predecessor ms
  if predecessor != nil AND ping(predecessor) fails:
    predecessor = nil

CHECK_SUCCESSOR():                  // runs every T_check_successor ms
  if ping(successor) fails:
    for each s in SuccList:
      if ping(s) succeeds:
        successor = s
        break
```

### 4.4 Join Sequence (Step by Step)

```
Step 1: n.find_successor(n.ID) via bootstrap → sets n.successor
Step 2: n.predecessor = nil
Step 3: n.init_finger_table()
Step 4: n.update_others()
        → for i=0..159: find predecessor of (n - 2^i)
          → call that node: update_finger_table(n, i)
Step 5: n.migrate_keys()
        → n.successor transfers keys ∈ (n.predecessor, n] to n

EMIT_EVENT: {type:"node_join", nodeID, fingerTable, successor, predecessor}
```

### 4.5 Leave Sequence (Graceful)

```
Step 1: Transfer all keys in n.Store → n.successor (bulk RPC)
Step 2: n.successor.set_predecessor(n.predecessor)
Step 3: n.predecessor.set_successor(n.successor)
Step 4: Stop all background goroutines
Step 5: Deregister from orchestrator

EMIT_EVENT: {type:"node_leave", nodeID, keysTransferred}
```

### 4.6 Crash Detection

```
Detection: CHECK_SUCCESSOR finds successor unreachable
Recovery:
  1. Walk SuccList (r=8 entries), find first live node
  2. Set successor = first_live
  3. Run STABILIZE immediately
  4. Broadcast fix_fingers to trigger ring repair
  
Detection: CHECK_PREDECESSOR finds predecessor unreachable
Recovery:
  1. predecessor = nil
  2. Wait for NOTIFY from new predecessor via stabilization
```

### 4.7 Timing Parameters (Defaults)

| Parameter | Default | Description |
|-----------|---------|-------------|
| `T_stabilize` | 500ms | Stabilize loop interval |
| `T_fix_fingers` | 1000ms | Fix-fingers loop interval |
| `T_check_predecessor` | 750ms | Predecessor liveness check |
| `T_check_successor` | 750ms | Successor liveness check |
| `successor_list_size` | 8 | Length of SuccList |
| `finger_bits` | 160 | Hash space bits (m) |

---

## 5. Kademlia Protocol Specification

### 5.1 Data Structures

```go
// XOR distance: d(a, b) = a XOR b, interpreted as big-endian uint160
type NodeID [20]byte

func (a NodeID) XORDistance(b NodeID) *big.Int

type Contact struct {
    ID       NodeID
    Addr     string
    LastSeen time.Time
}

type KBucket struct {
    mu       sync.Mutex
    entries  []Contact   // ordered: least-recently-seen first
    capacity int         // k=20
}

type RoutingTable struct {
    Self    NodeID
    Buckets [160]KBucket
    mu      sync.RWMutex
}

type KademliaNode struct {
    ID           NodeID
    Addr         string
    RoutingTable *RoutingTable
    Store        *KVStore
    
    Alpha        int // concurrency parameter, default 3
    K            int // replication factor / bucket size, default 20
    
    stopCh       chan struct{}
    EventBus     EventEmitter
}
```

### 5.2 Routing Table: k-Bucket Selection

For a contact `c` seen by node `n`:
```
bucket_index = 159 - clz(n.ID XOR c.ID)
  where clz = count leading zeros of the XOR result
  (bucket 0 = closest, bucket 159 = farthest)
```

**Bucket update on contact:**
```
UPDATE_BUCKET(contact):
  b = bucket_for(contact.ID)
  if contact already in b.entries:
    move contact to tail (most-recently-seen)
  else if len(b.entries) < k:
    append contact to tail
  else:
    // LRU eviction with liveness preference
    oldest = b.entries[0]
    if ping(oldest) responds:
      move oldest to tail; discard contact  // prefer live old nodes
    else:
      evict oldest; append contact          // replace dead node
```

**Bucket splitting:**
When own-ID bucket fills beyond k, split into two sub-buckets (relaxed routing table). Keep subtree of ≥ k nodes around own ID.

### 5.3 Four Core RPCs

```protobuf
// PING: liveness check + routing table update
rpc Ping(PingRequest) returns (PingResponse);

// STORE: store a key-value pair
rpc Store(StoreRequest) returns (StoreResponse);

// FIND_NODE: return k closest known contacts to targetID
rpc FindNode(FindNodeRequest) returns (FindNodeResponse);
// Response: list of ≤k Contact objects closest to target

// FIND_VALUE: return value if held, else return k closest contacts
rpc FindValue(FindValueRequest) returns (FindValueResponse);
// Response: {value: bytes, found: bool, contacts: []Contact}
```

**Side effect of all RPCs:** sender's contact info added/updated in recipient's routing table.

### 5.4 Iterative Lookup Algorithm

```
ITERATIVE_FIND_NODE(targetID):
  α = 3  // concurrency parameter
  k = 20 // result set size
  
  Pq = {}   // queried peers (set)
  Pn = k_closest_from_routing_table(targetID)  // sorted by XOR distance
  closest_seen = Pn[0]
  
  loop:
    candidates = Pn - Pq  // unqueried peers
    if len(candidates) == 0: break
    
    // Send α parallel FIND_NODE RPCs
    batch = candidates[:min(α, len(candidates))]
    results = parallel_rpc(FIND_NODE, batch, targetID)
    mark batch as queried in Pq
    
    for each result_set in results:
      for each contact in result_set:
        if contact not in Pn:
          insert into Pn (maintaining sorted order by XOR dist)
        update_bucket(contact)
    
    new_closest = Pn[0]
    if new_closest == closest_seen:
      // No improvement — query all remaining unqueried in Pn[:k]
      remaining = Pn[:k] - Pq
      parallel_rpc(FIND_NODE, remaining, targetID)
      break
    closest_seen = new_closest
  
  return Pn[:k]  // k closest found nodes
  
EMIT_EVENT per hop: {type:"kad_hop", fromNode, toNode, targetID, hopIndex}
```

**FIND_VALUE** is identical but RPCs use `FIND_VALUE`; stops early if any node returns the value.

### 5.5 STORE Operation

```
STORE(key, value):
  targets = ITERATIVE_FIND_NODE(key)  // find k closest nodes
  for each node in targets:
    node.STORE(key, value)            // parallel RPCs
  
  // Republication: originating node republishes every RepublishInterval
  // Values expire after TTL (default 24h)
```

### 5.6 Node Join

```
JOIN(bootstrap_contact):
  1. Add bootstrap to routing table (bucket update)
  2. ITERATIVE_FIND_NODE(self.ID)      // self-lookup, populates buckets
  3. for each bucket b (farthest first):
       if b is empty or stale:
         ITERATIVE_FIND_NODE(random_id_in_bucket_range(b))
         // "bucket refresh"
  
  EMIT_EVENT: {type:"node_join", nodeID, kBuckets}
```

### 5.7 Kademlia Timing Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `k` | 20 | Bucket size / replication factor |
| `alpha` | 3 | Concurrency in iterative lookup |
| `T_republish` | 24s (demo) / 24h (prod) | Key republication interval |
| `T_expire` | 25s (demo) / 25h (prod) | Key expiry TTL |
| `T_bucket_refresh` | 60s | Stale bucket refresh interval |
| `T_ping_timeout` | 300ms | RPC timeout for liveness |

---

## 6. Replication & Consistency Model

### 6.1 Replication Factor

- **Chord:** replicate to `R` immediate successors (default R=3). On write, coordinator node fans out to next R-1 successors.
- **Kademlia:** native k-replication; STORE is issued to k closest nodes.
- Configurable at runtime via gateway API.

### 6.2 Quorum Protocol

```
N = replication factor
W = write quorum (default 2)
R = read quorum  (default 2)

Guarantee: W + R > N → no stale read
Strong consistency: W = N (all replicas must ack)
High availability: W = 1 (fire and forget)

WRITE(key, value):
  replicas = find_replicas(key)
  acks = parallel_store(replicas, key, value, vector_clock)
  if len(acks) >= W: return success
  else: return quorum_failure

READ(key):
  replicas = find_replicas(key)
  responses = parallel_get(replicas, key, count=R)
  if len(responses) >= R:
    return resolve_conflict(responses)  // via vector clock comparison
  else: return quorum_failure
```

### 6.3 Vector Clocks

Every value entry carries a vector clock:

```go
type VectorClock map[string]uint64  // nodeID -> lamport counter

type ValueEntry struct {
    Key         [20]byte
    Value       []byte
    Clock       VectorClock
    Timestamp   time.Time          // wall clock for LWW fallback
    Version     uint64
    TTL         time.Duration
}

// Clock comparison results
type ClockOrder int
const (
    Before     ClockOrder = iota  // a < b (a is causally before b)
    After                          // a > b
    Concurrent                     // neither dominates → conflict
    Equal
)

func Compare(a, b VectorClock) ClockOrder
func Merge(a, b VectorClock) VectorClock
func Increment(c VectorClock, nodeID string) VectorClock
```

**Conflict resolution:**
1. If `Compare(a, b) == Before/After`: accept the newer (causally later) version
2. If `Concurrent`: apply LWW (last-write-wins by `Timestamp`); log conflict event

### 6.4 Anti-Entropy (Merkle Gossip)

```go
// Each node maintains a Merkle tree over its key-value store
// Leaves: SHA-1(key + value + clock)
// Internal nodes: SHA-1(left_child + right_child)
// Root hash represents entire store state

ANTI_ENTROPY_SYNC(peer):
  1. Exchange root hashes
  2. If equal: done (stores identical)
  3. Binary tree descent: find differing subtrees
  4. Exchange and merge differing key-value pairs
  5. Apply vector clock resolution on conflicts
  
  EMIT_EVENT: {type:"gossip_sync", fromNode, toNode, keysReconciled, divergentBefore, divergentAfter}
```

Anti-entropy runs every `T_gossip = 2s` (configurable). Each cycle selects 2 random peers.

---

## 7. Transport & RPC Layer

### 7.1 Protobuf Definitions

```protobuf
syntax = "proto3";
package dht;

// Shared types
message NodeInfo {
  bytes id   = 1;  // 20-byte SHA-1
  string addr = 2;
}

// --- Chord RPCs ---
service ChordService {
  rpc FindSuccessor   (FindSuccessorRequest)   returns (FindSuccessorResponse);
  rpc GetPredecessor  (GetPredecessorRequest)  returns (GetPredecessorResponse);
  rpc Notify          (NotifyRequest)          returns (NotifyResponse);
  rpc GetSuccessorList(GetSuccListRequest)     returns (GetSuccListResponse);
  rpc TransferKeys    (TransferKeysRequest)    returns (TransferKeysResponse);
  rpc BulkStore       (BulkStoreRequest)       returns (BulkStoreResponse);
  rpc Ping            (PingRequest)            returns (PingResponse);
}

// --- Kademlia RPCs ---
service KademliaService {
  rpc Ping      (PingRequest)      returns (PingResponse);
  rpc Store     (StoreRequest)     returns (StoreResponse);
  rpc FindNode  (FindNodeRequest)  returns (FindNodeResponse);
  rpc FindValue (FindValueRequest) returns (FindValueResponse);
}

// --- Common RPCs (quorum layer) ---
service ReplicationService {
  rpc Get    (GetRequest)    returns (GetResponse);
  rpc Put    (PutRequest)    returns (PutResponse);
  rpc Delete (DeleteRequest) returns (DeleteResponse);
}
```

### 7.2 In-Process Transport

For the demo/simulation, all nodes run in the same process. An in-process transport registry maps `nodeID → direct Go interface call` instead of real TCP, enabling zero-latency, no-serialization RPC with optional **simulated delay injection**:

```go
type Transport interface {
    FindSuccessor(ctx context.Context, target RemoteNode, id [20]byte) (RemoteNode, error)
    Notify(ctx context.Context, target RemoteNode, sender RemoteNode) error
    // ... all RPCs
}

type InProcessTransport struct {
    registry map[string]NodeHandler  // addr -> local node
    delay    time.Duration            // simulated latency per hop
    lossRate float64                  // simulated packet loss
}

type TCPTransport struct {
    // Real gRPC TCP implementation (for multi-process mode)
}
```

### 7.3 Simulated Failure Injection

```go
type FaultConfig struct {
    NodeCrash    bool          // simulate crash (stop goroutines, no cleanup)
    NetworkDelay time.Duration // per-hop latency injection
    PacketLoss   float64       // probability 0.0–1.0 of RPC failure
    Partition    [][]string    // list of partitioned groups by nodeAddr
}
```

---

## 8. Gateway API Specification

### 8.1 REST Endpoints

```
Base URL: http://localhost:8080/api/v1

# Network Management
POST   /network/start          Body: {protocol, nodeCount, m, virtualNodes}
POST   /network/reset          Restart fresh
GET    /network/state          Full snapshot: all nodes, all keys, topology
GET    /network/config         Current config (W, R, N, delays, etc.)
PUT    /network/config         Update runtime config

# Node Operations
POST   /nodes                  Spawn new node  Body: {id?, addr}
DELETE /nodes/:nodeID          Graceful leave
POST   /nodes/:nodeID/crash    Simulate crash
GET    /nodes/:nodeID          Node state (finger table / k-buckets, store, successor, etc.)

# Key-Value Operations
POST   /kv                     Insert  Body: {key, value}
GET    /kv/:key                Read (quorum)
DELETE /kv/:key                Delete
GET    /kv/:key/replicas       Show which nodes hold this key + their vector clocks

# Lookup Operations
POST   /lookup                 Body: {key}  → trace lookup path, return hops[]
GET    /lookup/trace/:traceID  Retrieve stored trace

# Simulation Scenarios
POST   /scenarios/:name/run    Run named scenario (bootstrap, churn, partition, hotkey)
GET    /scenarios              List available scenarios

# Metrics
GET    /metrics                Prometheus text format
GET    /metrics/json           JSON summary
```

### 8.2 WebSocket Protocol

```
Endpoint: ws://localhost:8080/ws

Client → Server (subscribe to event types):
{
  "action": "subscribe",
  "events": ["node_join", "node_leave", "lookup_hop", "stabilize", "key_migrate", "gossip", "conflict"]
}

Server → Client (event stream):
{
  "type": "node_join",
  "timestamp": "2024-01-01T00:00:00Z",
  "payload": {
    "nodeID": "deadbeef...",
    "addr": "127.0.0.1:7001",
    "successor": "...",
    "predecessor": "...",
    "fingerTable": [...]
  }
}

Event Types:
  node_join          New node joined the ring
  node_leave         Node gracefully left
  node_crash         Node crashed (no cleanup)
  stabilize          Stabilization cycle completed (Chord)
  fix_fingers        Finger table entry updated
  notify             Predecessor updated via notify
  key_migrate        Keys moved between nodes (join/leave)
  lookup_start       Lookup initiated {key, fromNode}
  lookup_hop         Single hop in lookup {fromNode, toNode, fingerUsed, hopIndex}
  lookup_complete    Lookup finished {key, targetNode, totalHops, latencyMs}
  write_quorum       Write quorum attempt {key, W, N, acksReceived, success}
  read_quorum        Read quorum attempt {key, R, N, responsesReceived, conflict}
  vector_clock       Value version update {key, nodeID, oldClock, newClock}
  conflict_detected  Concurrent write conflict {key, versions[]}
  gossip_sync        Anti-entropy sync completed {fromNode, toNode, keysReconciled}
  bucket_update      Kademlia k-bucket updated {nodeID, bucketIndex, contact}
  republish          Kademlia key republished {key, nodeID}
  ring_state         Full ring snapshot (sent on subscribe and every 5s)
```

---

## 9. Frontend Specification

### 9.1 Views & Routing

```
/                  → Ring Visualizer (main view)
/lookup            → Lookup Tracer
/nodes/:id         → Node Inspector
/consistency       → Consistency & Replication Dashboard
/metrics           → Performance Metrics
/scenarios         → Scenario Runner
```

### 9.2 Ring Visualizer (D3.js)

**Canvas:** Full-screen SVG. Ring drawn as circle with circumference representing key space 0..2^m.

**Node rendering:**
```
Physical node: filled circle, radius=12
  - Color: green (healthy) / yellow (stabilizing) / red (unreachable)
  - Heat fill: interpolate green→red based on key_count / avg_key_count
  - Label: first 6 hex chars of node ID
  
Virtual node: smaller circle (radius=6), same color as physical, 
  linked by thin dashed line to physical node label
  
Key: tiny dot on ring circumference at hash(key) position
  - Color by replication count: grey(1) / yellow(2) / green(≥3)
```

**Pointer rendering:**
```
Successor arrow:   solid arc, clockwise from node to successor
Predecessor arrow: dashed arc, counter-clockwise
Finger table rays: faint arcs from node to each finger (toggled via checkbox)
  - Finger 0 (immediate successor): opacity 1.0
  - Finger i: opacity max(0.1, 1.0 - i*0.006)
```

**Lookup animation:**
```
On lookup_start:  highlight origin node
On lookup_hop:    draw animated pulse along arc from→to
  - Pulse: filled circle traveling along arc, 300ms transition
  - Leave glowing trail on traversed arcs
On lookup_complete: flash destination node green; show hop count badge
```

**Stabilization animation:**
```
On stabilize event: flash affected successor/predecessor arrows
On fix_fingers:     briefly highlight updated finger ray
On key_migrate:     animate dots moving along ring arc from source to destination
```

**Interactions:**
- Hover node: tooltip with ID, successor, predecessor, key count
- Click node: navigate to `/nodes/:id`
- Click key dot: highlight all replica nodes holding this key
- Drag canvas: pan
- Scroll: zoom in/out on ring arc

### 9.3 Node Inspector

```
Panel sections:
  Header:       NodeID (hex), Addr, Protocol, Status badge
  
  Routing:      Chord mode: successor, predecessor, SuccessorList
                Kademlia mode: k-bucket table (160 rows, lazy-render)
  
  Finger Table: Scrollable table: index | start | node ID | addr
                (Chord only — highlight last updated entry)
  
  K-Buckets:    (Kademlia only) Tree view: 
                  Bucket 0 (closest): [contact1, contact2, ...]
                  Bucket 1: [...]
                  ...
                Shows: last_seen, contact count vs k
  
  Store:        Table of key-value pairs this node owns
                Columns: key_hex | value_preview | vector_clock | TTL | replicated_at
  
  Events:       Live stream of events involving this node (WebSocket filtered)
  
  Actions:      [Kill Node] [Crash Node] [Insert Key Here]
```

### 9.4 Lookup Tracer

```
Input:  text field → SHA-1 hash shown live as user types
        [Trace Lookup] button
        Protocol selector: Chord | Kademlia

Animation panel:
  - Small ring diagram showing only nodes involved in this lookup
  - Hop list: numbered steps with from/to node, which finger/bucket used, decision reason
  - Timeline bar showing parallel RPCs (Kademlia α-concurrent)
  - Final result: target node highlighted, hop count badge

Comparison mode:
  - Toggle [Compare Chord vs Kademlia]
  - Show both traces side-by-side
  - Metrics: Chord hops vs Kademlia hops, total latency
```

### 9.5 Consistency Dashboard

```
Left panel — Replication State:
  For each key in the system:
    Key row: key_hex | intended_replicas | actual_replicas | under_replicated_badge
    Expand: show each replica node, its value, vector clock, last_updated

Center panel — Vector Clock Inspector:
  Select a key → show version history
  Timeline: each write as a node in a DAG
    - Nodes: (nodeID, timestamp, clock_value)
    - Edges: causal dependencies
    - Red edge: concurrent conflict → resolution shown
  
Right panel — Anti-Entropy Log:
  Live feed of gossip sync events
  Per sync: {nodeA ↔ nodeB, keys_exchanged, conflicts_resolved, duration_ms}

Bottom panel — Convergence Meter:
  After any write, show % of replicas that have converged
  Time-series: convergence curve (X=time, Y=% consistent replicas)
  Shows partition + heal simulation effect visually
```

### 9.6 Technology Stack

| Concern | Library |
|---------|---------|
| Framework | React 18 + TypeScript 5 |
| Build | Vite 5 |
| Ring visualization | D3.js v7 |
| Charts/metrics | Recharts |
| State management | Zustand |
| Data fetching | TanStack Query v5 |
| WebSocket | native `useWebSocket` custom hook |
| UI components | shadcn/ui + Radix UI |
| Styling | Tailwind CSS |
| Icons | Lucide React |
| Routing | React Router v6 |
| Code gen | openapi-typescript (from gateway OpenAPI spec) |

---

## 10. Data Models & Schemas

### 10.1 Go Types

```go
// --- Core types ---
type NodeID [20]byte

type VectorClock map[string]uint64

type ValueEntry struct {
    Key       NodeID
    Value     []byte
    Clock     VectorClock
    Timestamp time.Time
    TTL       time.Duration
}

type KVStore struct {
    mu      sync.RWMutex
    entries map[NodeID]*ValueEntry
    merkle  *MerkleTree
}

// --- Event types (emitted to event bus) ---
type EventType string

type Event struct {
    Type      EventType       `json:"type"`
    Timestamp time.Time       `json:"timestamp"`
    Payload   json.RawMessage `json:"payload"`
}

// --- Gateway REST models ---
type NetworkState struct {
    Protocol   string          `json:"protocol"`
    Nodes      []NodeSnapshot  `json:"nodes"`
    Keys       []KeySnapshot   `json:"keys"`
    Config     Config          `json:"config"`
}

type NodeSnapshot struct {
    ID          string          `json:"id"`
    Addr        string          `json:"addr"`
    Successor   *string         `json:"successor"`
    Predecessor *string         `json:"predecessor"`
    FingerTable []FingerEntry   `json:"fingerTable,omitempty"`
    KBuckets    []KBucketInfo   `json:"kBuckets,omitempty"`
    KeyCount    int             `json:"keyCount"`
    Status      string          `json:"status"`
}
```

### 10.2 Config Schema

```go
type Config struct {
    Protocol         string        `json:"protocol"`          // "chord" | "kademlia"
    M                int           `json:"m"`                 // hash bits, default 160
    VirtualNodes     int           `json:"virtualNodes"`      // vnodes per physical, default 3
    ReplicationN     int           `json:"replicationN"`      // N, default 3
    WriteQuorum      int           `json:"writeQuorum"`       // W, default 2
    ReadQuorum       int           `json:"readQuorum"`        // R, default 2
    SuccessorListSize int          `json:"successorListSize"` // Chord, default 8
    KBucketSize      int           `json:"kBucketSize"`       // Kademlia k, default 20
    Alpha            int           `json:"alpha"`             // Kademlia α, default 3
    StabilizeInterval time.Duration `json:"stabilizeInterval"` // default 500ms
    FixFingersInterval time.Duration `json:"fixFingersInterval"` // default 1s
    GossipInterval   time.Duration  `json:"gossipInterval"`    // default 2s
    RepublishInterval time.Duration `json:"republishInterval"` // default 24s (demo)
    SimDelay         time.Duration  `json:"simDelay"`          // per-hop delay, default 0
    SimLossRate      float64        `json:"simLossRate"`       // packet loss 0.0-1.0
}
```

---

## 11. Configuration Reference

All values overridable via environment variables or `config.yaml`:

```yaml
# config.yaml
server:
  port: 8080
  cors_origins: ["http://localhost:5173"]

dht:
  protocol: chord          # chord | kademlia
  m: 160                   # hash space bits
  virtual_nodes: 3
  replication_n: 3
  write_quorum: 2
  read_quorum: 2

chord:
  stabilize_interval: 500ms
  fix_fingers_interval: 1s
  check_predecessor_interval: 750ms
  successor_list_size: 8

kademlia:
  k: 20
  alpha: 3
  republish_interval: 24s
  expire_ttl: 25s
  bucket_refresh_interval: 60s

simulation:
  initial_nodes: 6
  sim_delay: 0ms
  sim_loss_rate: 0.0

logging:
  level: info             # debug | info | warn | error
  format: json
```

---

## 12. Error Handling & Fault Tolerance

### 12.1 RPC Error Taxonomy

| Error Code | Meaning | Handling |
|------------|---------|----------|
| `NODE_UNREACHABLE` | Target node not responding | Mark dead, retry via successor list |
| `QUORUM_FAILURE` | Not enough replicas acked | Return error to client; log |
| `KEY_NOT_FOUND` | Key does not exist in DHT | Return 404 |
| `RING_UNSTABLE` | Ring mid-stabilization | Retry with backoff (max 3 retries) |
| `CLOCK_CONFLICT` | Concurrent write detected | Resolve via LWW; log conflict event |
| `PARTITION_DETECTED` | Network split | Serve reads from available partition; queue writes |

### 12.2 Retry Policy

```go
type RetryConfig struct {
    MaxAttempts int           // 3
    BaseDelay   time.Duration // 100ms
    MaxDelay    time.Duration // 2s
    Multiplier  float64       // 2.0 (exponential backoff)
    Jitter      bool          // true
}
```

### 12.3 Graceful Degradation

- If W nodes unreachable during write → write to available nodes + log replication lag
- If R nodes unreachable during read → return best available value + `"degraded": true` flag
- If ring has ≤ 2 nodes: disable stabilization assertions; accept singleton or pair

---

## 13. Testing Strategy

### 13.1 Unit Tests

```
internal/consistent/   hash_test.go      → hash function, interval arithmetic, vnode distribution
internal/chord/        chord_test.go     → find_successor correctness, finger table init, stabilize
internal/kademlia/     kademlia_test.go  → XOR distance, k-bucket LRU, iterative lookup convergence
internal/replication/  quorum_test.go    → vector clock comparison, conflict resolution, quorum math
internal/store/        store_test.go     → KV CRUD, TTL expiry, Merkle tree correctness
```

### 13.2 Integration Tests

```
test/integration/
  chord_ring_test.go    → 10-node ring: all keys reachable, O(log N) hops verified
  chord_churn_test.go   → add/remove 50% of nodes, verify 0 data loss with R=3
  kademlia_net_test.go  → 20-node network, all keys findable via iterative lookup
  quorum_test.go        → W=2 R=2 N=3: write then read, verify latest version returned
  partition_test.go     → split ring in half, write to both halves, heal, verify convergence
  gossip_test.go        → after split+heal, Merkle gossip reconciles within T_gossip * 3
```

### 13.3 Scenario Tests

```
test/scenarios/
  scenario_bootstrap_test.go   → 1→10 nodes: ring stabilizes within 5s
  scenario_hotkey_test.go      → 10k writes to 1 key on 3-vnode setup: even load distribution
  scenario_performance_test.go → N=10,50,100,500 nodes: lookup hops fit O(log N)
```

---

## 14. Performance Targets

| Metric | Target | Measurement |
|--------|--------|-------------|
| Lookup hops (Chord) | ≤ 1.5 × log₂(N) | Mean over 1000 lookups |
| Lookup hops (Kademlia) | ≤ log₂(N) + 1 | Mean over 1000 lookups |
| Stabilization convergence | < 3s after node join | All successors correct |
| Key migration on join | < 1s for ≤10k keys | Wall clock |
| Anti-entropy convergence | < 3 gossip cycles | After partition heal |
| Ring state broadcast | ≤ 100ms | WebSocket event latency |
| REST API P99 latency | < 50ms | For non-lookup endpoints |
| Frontend re-render | < 16ms | For ring state update ≤50 nodes |

---

## 15. Project File Structure

```
dht-system/
├── cmd/
│   ├── node/
│   │   └── main.go              # Standalone node process (TCP mode)
│   └── gateway/
│       └── main.go              # HTTP/WS gateway + in-process simulation
│
├── internal/
│   ├── consistent/
│   │   ├── hash.go              # SHA-1/256, ring arithmetic, interval checks
│   │   ├── vnode.go             # Virtual node management
│   │   └── hash_test.go
│   │
│   ├── chord/
│   │   ├── node.go              # ChordNode struct + lifecycle
│   │   ├── finger.go            # Finger table init + fix_fingers
│   │   ├── stabilize.go         # stabilize(), notify(), check_predecessor()
│   │   ├── join.go              # Join sequence + key migration
│   │   ├── leave.go             # Graceful leave + crash handling
│   │   ├── lookup.go            # find_successor, closest_preceding_node
│   │   └── chord_test.go
│   │
│   ├── kademlia/
│   │   ├── node.go              # KademliaNode struct + lifecycle
│   │   ├── routing.go           # RoutingTable, k-buckets, bucket update/split
│   │   ├── lookup.go            # Iterative FIND_NODE / FIND_VALUE
│   │   ├── join.go              # Bootstrap + bucket refresh
│   │   ├── republish.go         # Key republication + expiry
│   │   └── kademlia_test.go
│   │
│   ├── replication/
│   │   ├── quorum.go            # Quorum read/write logic
│   │   ├── vclock.go            # Vector clock operations
│   │   ├── conflict.go          # LWW + merge resolution
│   │   └── quorum_test.go
│   │
│   ├── store/
│   │   ├── store.go             # In-memory KV store with TTL
│   │   ├── merkle.go            # Merkle tree for anti-entropy
│   │   └── store_test.go
│   │
│   ├── gossip/
│   │   ├── gossip.go            # Anti-entropy Merkle sync protocol
│   │   └── gossip_test.go
│   │
│   ├── transport/
│   │   ├── interface.go         # Transport interface definition
│   │   ├── inprocess.go         # In-process transport (simulation)
│   │   ├── tcp.go               # Real gRPC TCP transport
│   │   └── fault.go             # Fault injection wrapper
│   │
│   ├── simulation/
│   │   ├── orchestrator.go      # Create/destroy nodes, manage network
│   │   └── scenarios.go         # Named simulation scenarios
│   │
│   └── events/
│       ├── bus.go               # Event bus (pub/sub, fan-out to WS)
│       └── types.go             # All event type definitions
│
├── gateway/
│   ├── server.go                # gin/chi HTTP server setup
│   ├── rest.go                  # REST route handlers
│   ├── websocket.go             # WebSocket hub + broadcast
│   └── middleware.go            # CORS, logging, recovery
│
├── proto/
│   ├── chord.proto
│   ├── kademlia.proto
│   └── common.proto
│
├── frontend/
│   ├── src/
│   │   ├── components/
│   │   │   ├── RingVisualizer/
│   │   │   │   ├── RingCanvas.tsx       # D3 SVG ring
│   │   │   │   ├── NodeMarker.tsx
│   │   │   │   ├── FingerRays.tsx
│   │   │   │   ├── LookupAnimation.tsx
│   │   │   │   └── KeyDots.tsx
│   │   │   ├── NodeInspector/
│   │   │   ├── LookupTracer/
│   │   │   ├── ConsistencyDash/
│   │   │   ├── MetricsPanel/
│   │   │   └── ScenarioRunner/
│   │   ├── hooks/
│   │   │   ├── useWebSocket.ts
│   │   │   ├── useRingState.ts
│   │   │   └── useLookupTrace.ts
│   │   ├── store/
│   │   │   └── dhtStore.ts           # Zustand store
│   │   ├── api/
│   │   │   └── client.ts             # REST API client
│   │   ├── types/
│   │   │   └── dht.ts                # TypeScript types
│   │   └── App.tsx
│   ├── package.json
│   └── vite.config.ts
│
├── config.yaml
├── docker-compose.yml
├── Makefile
├── go.mod
├── go.sum
├── SPEC.md
├── CHECKLIST.md
└── README.md
```