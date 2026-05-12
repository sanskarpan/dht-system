# CHECKLIST.md — DHT System Implementation Tracker
## Chord / Kademlia P2P Distributed Hash Table

> Track every implementation task. Check boxes as you complete them.
> Legend: 🔴 Blocking · 🟡 Important · 🟢 Enhancement · 🔵 Stretch

---

## Phase 0 — Project Bootstrap

### 0.1 Repository Setup
- [x] 🔴 Initialize Go module: `go mod init github.com/sanskarpan/dht-system/dht-system`
- [x] 🔴 Create directory structure as defined in SPEC.md §15
- [x] 🔴 Add `config.yaml` with all defaults from SPEC.md §11
- [x] 🔴 Create `Makefile` with targets: `build`, `test`, `run`, `lint`, `proto-gen`
- [x] 🔴 Add `.gitignore` (Go + Node)
- [x] 🟡 Create `docker-compose.yml` for gateway + frontend

### 0.2 Go Dependencies
- [x] 🔴 `google.golang.org/grpc` + `google.golang.org/protobuf`
- [x] 🔴 `github.com/gin-gonic/gin` (HTTP server)
- [x] 🔴 `github.com/gorilla/websocket`
- [x] 🔴 `github.com/prometheus/client_golang`
- [x] 🟡 `go.uber.org/zap` (structured logging)
- [x] 🟡 `github.com/spf13/viper` (config loading)
- [x] 🟡 `golang.org/x/sync` (concurrent RPC fanout)

### 0.3 Protobuf Setup
- [x] 🔴 Install `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc`
- [x] 🔴 Write `proto/common.proto` (NodeInfo, ValueEntry, VectorClock)
- [x] 🔴 Write `proto/chord.proto` (all Chord RPCs)
- [x] 🔴 Write `proto/kademlia.proto` (all Kademlia RPCs)
- [x] 🔴 Add `make proto-gen` target, generate Go stubs
- [x] 🔴 Verify generated code compiles cleanly

### 0.4 Frontend Bootstrap
- [x] 🔴 `npm create vite@latest frontend -- --template react-ts`
- [x] 🔴 Install: `d3`, `zustand`, `@tanstack/react-query`, `recharts`
- [x] 🔴 Install: `tailwindcss`, `lucide-react`, `react-router-dom`
- [x] 🔴 Configure Tailwind + path aliases in `vite.config.ts`
- [x] 🔴 Set up React Router routes (/, /lookup, /nodes/:id, /consistency, /metrics, /scenarios)
- [x] 🔴 Create placeholder components for all 6 views

---

## Phase 1 — Consistent Hashing Foundation

### 1.1 Hash Ring (`internal/consistent/`)
- [x] 🔴 `hash.go` — SHA-1 hash function returning `[20]byte`
- [x] 🔴 `hash.go` — `NodeIDFromAddr(addr string) [20]byte`
- [x] 🔴 `hash.go` — `KeyID(key string) [20]byte`
- [x] 🔴 `hash.go` — `IsInInterval(id, start, end [20]byte) bool` (exclusive both ends)
- [x] 🔴 `hash.go` — `IsInIntervalRightClosed(id, start, end [20]byte) bool`
- [x] 🔴 `hash.go` — Ring arithmetic: `Add(id [20]byte, n *big.Int) [20]byte`
- [x] 🔴 `hash.go` — `PowerOfTwo(i int) *big.Int` — computes 2^i
- [x] 🟡 `hash.go` — `IDToHex(id [20]byte) string` (for display)
- [x] 🟡 `hash.go` — `IDToFloat(id [20]byte) float64` — position on [0,1] ring for visualization

### 1.2 Virtual Nodes (`internal/consistent/`)
- [x] 🟡 `vnode.go` — `VNodeID(addr string, index int) [20]byte`
- [x] 🟡 `vnode.go` — `VNodeRing` struct: maps physical nodes to virtual positions
- [x] 🟡 `vnode.go` — `FindOwner(keyID [20]byte) string` — returns physical node addr
- [x] 🟡 `vnode.go` — `LoadBalance() map[string]int` — key count per physical node

### 1.3 Tests
- [x] 🔴 Hash function produces uniform distribution (chi-squared test over 10k keys)
- [x] 🔴 `IsInInterval` correct for wrap-around cases (e.g., start > end mod 2^m)
- [x] 🔴 Ring arithmetic: `(id + 2^i) mod 2^m` computed correctly for boundary cases
- [x] 🟡 Virtual nodes improve load balance (std_dev / mean < 0.3 for 3 vnodes)

---

## Phase 2 — Transport Layer

### 2.1 Interface (`internal/transport/`)
- [x] 🔴 `interface.go` — Define `Transport` interface with all RPC methods
- [x] 🔴 `interface.go` — Define `NodeHandler` interface (what each node exposes)

### 2.2 In-Process Transport
- [x] 🔴 `inprocess.go` — `InProcessTransport` struct with node registry
- [x] 🔴 `inprocess.go` — `Register(addr string, handler NodeHandler)`
- [x] 🔴 `inprocess.go` — `Deregister(addr string)`
- [x] 🔴 `inprocess.go` — All RPC methods: direct call into registry (no serialization)
- [x] 🟡 `inprocess.go` — Configurable latency injection (`time.Sleep(delay)` per call)
- [x] 🟡 `fault.go` — `FaultInjectingTransport` wrapping via inprocess delay/loss
- [x] 🟡 `fault.go` — Random packet loss (`rand.Float64() < lossRate → return error`)
- [x] 🟡 `fault.go` — Network partition: block RPC if src and dst in different partition groups

### 2.3 TCP Transport (gRPC)
- [x] 🟢 `tcp.go` — `TCPTransport` wrapping generated gRPC clients
- [x] 🟢 `tcp.go` — Connection pool with per-addr `grpc.ClientConn`
- [x] 🟢 `tcp.go` — Connection reuse + graceful close on deregister

---

## Phase 3 — KV Store

### 3.1 Core Store (`internal/store/`)
- [x] 🔴 `store.go` — `KVStore` struct with `sync.RWMutex`
- [x] 🔴 `store.go` — `Put(key [20]byte, entry *ValueEntry) error`
- [x] 🔴 `store.go` — `Get(key [20]byte) (*ValueEntry, bool)`
- [x] 🔴 `store.go` — `Delete(key [20]byte) bool`
- [x] 🔴 `store.go` — `GetKeysInRange(start, end [20]byte) []*ValueEntry`
- [x] 🔴 `store.go` — `All() []*ValueEntry`
- [x] 🟡 `store.go` — TTL expiry: background goroutine evicts expired entries
- [x] 🟡 `store.go` — `Size() int` — current entry count

### 3.2 Vector Clocks (`internal/replication/`)
- [x] 🔴 `vclock.go` — `VectorClock` type alias `map[string]uint64`
- [x] 🔴 `vclock.go` — `Increment(c VectorClock, nodeID string) VectorClock`
- [x] 🔴 `vclock.go` — `Compare(a, b VectorClock) ClockOrder` (Before/After/Concurrent/Equal)
- [x] 🔴 `vclock.go` — `Merge(a, b VectorClock) VectorClock` (component-wise max)
- [x] 🔴 `conflict.go` — `Resolve(entries []*ValueEntry) *ValueEntry` (LWW on conflict)

### 3.3 Merkle Tree (`internal/store/`)
- [x] 🟡 `merkle.go` — `MerkleTree` struct over sorted key-value pairs
- [x] 🟡 `merkle.go` — `Root() [20]byte` — current root hash
- [x] 🟡 `merkle.go` — `Update(key [20]byte, value []byte)` — recompute affected path
- [x] 🟡 `merkle.go` — `Diff(other *MerkleTree) [][20]byte` — return differing leaf keys
- [x] 🟡 `merkle_test.go` — Correct root after insert/update/delete

---

## Phase 4 — Chord Protocol

### 4.1 Node Core (`internal/chord/`)
- [x] 🔴 `node.go` — `ChordNode` struct (see SPEC §4.1)
- [x] 🔴 `node.go` — `NewChordNode(addr string, cfg *Config, transport Transport, bus EventEmitter) *ChordNode`
- [x] 🔴 `node.go` — `Start()` — start all background goroutines
- [x] 🔴 `node.go` — `Stop()` — graceful shutdown, close stopCh

### 4.2 Finger Table (`internal/chord/`)
- [x] 🔴 `finger.go` — `initFingerTable(bootstrap *RemoteNode) error`
- [x] 🔴 `finger.go` — `fingerStart(i int) [20]byte` — `(n + 2^i) mod 2^m`
- [x] 🔴 `finger.go` — `fixFingers()` — randomly refresh one finger entry
- [x] 🔴 `finger.go` — `GetFinger(i int) *RemoteNode`
- [x] 🔴 `finger.go` — `SetFinger(i int, node *RemoteNode)`
- [x] 🟡 `finger.go` — `UpdateFingerTable(s *RemoteNode, i int)` — for update_others() RPC

### 4.3 Lookup (`internal/chord/`)
- [x] 🔴 `lookup.go` — `FindSuccessor(id [20]byte) (*RemoteNode, []HopEvent, error)` — returns path
- [x] 🔴 `lookup.go` — `ClosestPrecedingNode(id [20]byte) *RemoteNode`
- [x] 🔴 `lookup.go` — Emit `lookup_hop` event per hop
- [x] 🔴 `lookup.go` — Max hop guard: if hops > 3*m, return error (prevent infinite loop)

### 4.4 Stabilization (`internal/chord/`)
- [x] 🔴 `stabilize.go` — `Stabilize()` — core stabilize loop body
- [x] 🔴 `stabilize.go` — `Notify(n *RemoteNode)` — handle incoming notify RPC
- [x] 🔴 `stabilize.go` — `CheckPredecessor()` — ping predecessor, nil if dead
- [x] 🔴 `stabilize.go` — `CheckSuccessor()` — walk SuccList on failure
- [x] 🔴 `stabilize.go` — Background runner goroutine with configurable tick interval
- [x] 🔴 `stabilize.go` — Emit `stabilize` event after each cycle
- [x] 🟡 `stabilize.go` — Emit `fix_fingers` event when finger updated

### 4.5 Join & Leave (`internal/chord/`)
- [x] 🔴 `join.go` — `Join(bootstrap *RemoteNode) error`
- [x] 🔴 `join.go` — `MigrateKeys()` — take keys from successor in range (predecessor, self]
- [x] 🔴 `join.go` — `UpdateOthers()` — notify affected nodes to update finger tables
- [x] 🔴 `leave.go` — `Leave() error` — graceful departure + key transfer
- [x] 🔴 `leave.go` — `SimulateCrash()` — abrupt stop, no cleanup
- [x] 🔴 `join.go` — Emit `node_join` event with full finger table snapshot
- [x] 🔴 `leave.go` — Emit `node_leave` event with keys transferred count

### 4.6 RPC Handler (Chord)
- [x] 🔴 Implement `NodeHandler` interface for `ChordNode`
- [x] 🔴 Handle: `FindSuccessor`, `GetPredecessor`, `Notify`, `GetSuccessorList`
- [x] 🔴 Handle: `TransferKeys` (bulk key migration), `Ping`, `UpdateFingerTable`

### 4.7 Chord Tests
- [x] 🔴 `chord_test.go` — `find_successor` correct for 8-node ring (verify all keys)
- [x] 🔴 `chord_test.go` — Lookup hops ≤ ⌈log₂(N)⌉ + 1 after ring stabilizes
- [x] 🔴 `chord_test.go` — After join: all keys in (predecessor, newNode] migrated
- [x] 🔴 `chord_test.go` — After leave: all keys still reachable via successor
- [x] 🔴 `chord_test.go` — After crash: SuccList used to find next live successor
- [x] 🟡 `chord_test.go` — Concurrent joins (3 simultaneous): ring eventually stable

---

## Phase 5 — Kademlia Protocol

### 5.1 Node Core (`internal/kademlia/`)
- [x] 🔴 `node.go` — `KademliaNode` struct (see SPEC §5.1)
- [x] 🔴 `node.go` — `NewKademliaNode(addr string, cfg *Config, transport Transport, bus EventEmitter)`
- [x] 🔴 `node.go` — `Start()` / `Stop()`

### 5.2 Routing Table (`internal/kademlia/`)
- [x] 🔴 `routing.go` — `RoutingTable` with 160 `KBucket` entries
- [x] 🔴 `routing.go` — `BucketIndex(self, contact NodeID) int` — `159 - clz(self XOR contact)`
- [x] 🔴 `routing.go` — `UpdateContact(contact Contact)` — insert/move-to-tail/ping-and-evict
- [x] 🔴 `routing.go` — `KClosest(target NodeID, k int) []Contact` — k contacts sorted by XOR dist
- [x] 🔴 `routing.go` — `XORDistance(a, b NodeID) *big.Int`
- [x] 🟡 `routing.go` — `SplitBucket(index int)` — split when own-ID bucket overflows
- [x] 🟡 `routing.go` — `RefreshBucket(index int)` — lookup random ID in bucket range

### 5.3 Iterative Lookup (`internal/kademlia/`)
- [x] 🔴 `lookup.go` — `IterativeFindNode(target NodeID) ([]Contact, []HopEvent, error)`
- [x] 🔴 `lookup.go` — α-concurrent parallel RPCs using WaitGroup
- [x] 🔴 `lookup.go` — Convergence detection: stop when k closest all queried
- [x] 🔴 `lookup.go` — `IterativeFindValue(key NodeID) (*ValueEntry, []Contact, []HopEvent, error)`
- [x] 🔴 `lookup.go` — Emit `lookup_hop` event per RPC batch
- [x] 🟡 `lookup.go` — Timeout per iteration: configurable `alpha_timeout`

### 5.4 Join & Republication (`internal/kademlia/`)
- [x] 🔴 `join.go` — `Join(bootstrap Contact) error`
- [x] 🔴 `join.go` — Self-lookup → bucket population
- [x] 🔴 `join.go` — Bucket refresh for all buckets farther than nearest neighbor
- [x] 🔴 `republish.go` — Background goroutine: republish all owned keys every `RepublishInterval`
- [x] 🔴 `republish.go` — TTL expiry: evict entries older than `ExpireTTL` (handled by store)
- [x] 🟡 `republish.go` — Caching: store looked-up values on nodes near key (closer than publisher)

### 5.5 RPC Handler (Kademlia)
- [x] 🔴 Implement `PING`, `STORE`, `FIND_NODE`, `FIND_VALUE` handler methods
- [x] 🔴 Every incoming RPC: update routing table with sender's contact info
- [x] 🔴 `FIND_VALUE`: check local store first; if not found return `FIND_NODE` results

### 5.6 Kademlia Tests
- [x] 🔴 `kademlia_test.go` — `XORDistance` is a proper metric (triangle inequality)
- [x] 🔴 `kademlia_test.go` — `BucketIndex` correct for known ID pairs
- [x] 🔴 `kademlia_test.go` — k-bucket LRU: prefer live old nodes over new nodes
- [x] 🔴 `kademlia_test.go` — `IterativeFindNode` converges within log₂(N)+2 iterations in 20-node network
- [x] 🔴 `kademlia_test.go` — Store+retrieve: key always findable after `JOIN` sequence
- [x] 🟡 `kademlia_test.go` — Concurrent lookup from 5 different nodes: same key found by all

---

## Phase 6 — Replication & Consistency

### 6.1 Quorum Layer (`internal/replication/`)
- [x] 🔴 `quorum.go` — `QuorumManager` struct with (N, W, R) config
- [x] 🔴 `quorum.go` — `Write(key string, value []byte) (*WriteResult, error)` — fan-out to N replicas, wait for W acks
- [x] 🔴 `quorum.go` — `Read(key string) (*ReadResult, error)` — fan-out to N replicas, collect R responses
- [x] 🔴 `quorum.go` — `findReplicas(key [20]byte) []*RemoteNode` — find N nodes responsible for key
- [x] 🔴 `quorum.go` — Parallel RPC with timeout; count acks; fail on quorum miss
- [x] 🔴 `quorum.go` — Emit `write_quorum` and `read_quorum` events
- [x] 🟡 `quorum.go` — Read-repair: after quorum read, update stale replicas with latest version

### 6.2 Conflict Resolution
- [x] 🔴 `conflict.go` — `Resolve(entries []*ValueEntry) *ValueEntry`
- [x] 🔴 `conflict.go` — If one entry causally dominates: return it
- [x] 🔴 `conflict.go` — If concurrent: apply LWW (latest `Timestamp`); emit `conflict_detected` event
- [x] 🟡 `conflict.go` — Log all conflicts to in-memory conflict history (last 100)

### 6.3 Anti-Entropy Gossip (`internal/gossip/`)
- [x] 🟡 `gossip.go` — `AntiEntropy` background goroutine
- [x] 🟡 `gossip.go` — Select 2 random peers per cycle
- [x] 🟡 `gossip.go` — Exchange Merkle root hashes → if equal, done
- [x] 🟡 `gossip.go` — Binary descent to find differing subtrees → exchange + merge entries
- [x] 🟡 `gossip.go` — Emit `gossip_sync` event with reconciliation stats
- [x] 🟡 `gossip_test.go` — After 3 gossip cycles post-partition: all nodes agree on all keys

---

## Phase 7 — Simulation Orchestrator

### 7.1 Orchestrator (`internal/simulation/`)
- [x] 🔴 `orchestrator.go` — `Orchestrator` struct: manages all nodes, shared transport, event bus
- [x] 🔴 `orchestrator.go` — `SpawnNode(addr string) (Node, error)` — create + join network
- [x] 🔴 `orchestrator.go` — `KillNode(addr string) error` — graceful leave
- [x] 🔴 `orchestrator.go` — `CrashNode(addr string) error` — abrupt stop
- [x] 🔴 `orchestrator.go` — `GetNetworkState() NetworkState` — full snapshot for REST API
- [x] 🔴 `orchestrator.go` — `Insert(key, value string) error` — quorum write
- [x] 🔴 `orchestrator.go` — `Lookup(key string) (*LookupTrace, error)` — traced lookup
- [x] 🟡 `orchestrator.go` — `SetPartition(groups [][]string)` — inject network partition
- [x] 🟡 `orchestrator.go` — `HealPartition()` — remove partition

### 7.2 Scenarios (`internal/simulation/`)
- [x] 🟡 `scenarios.go` — `ScenarioBootstrap` — 1 → 10 nodes, emit step events
- [x] 🟡 `scenarios.go` — `ScenarioChurn` — add/remove nodes continuously for 30s
- [x] 🟡 `scenarios.go` — `ScenarioPartition` — split/heal partition, measure convergence
- [x] 🟡 `scenarios.go` — `ScenarioHotKey` — write 1000 keys, show vnode load balance
- [x] 🟡 `scenarios.go` — `ScenarioBenchmark` — N=10..500 nodes, measure lookup hops

---

## Phase 8 — Event Bus

### 8.1 Event Bus (`internal/events/`)
- [x] 🔴 `bus.go` — `EventBus` with `chan Event` publish + multiple subscriber channels
- [x] 🔴 `bus.go` — `Publish(event Event)` — non-blocking send (drop if buffer full)
- [x] 🔴 `bus.go` — `Subscribe(types []EventType) <-chan Event`
- [x] 🔴 `bus.go` — `Unsubscribe(ch <-chan Event)`
- [x] 🔴 `types.go` — Define all event payload structs (see SPEC §8.2)
- [x] 🟡 `bus.go` — Event history ring buffer (last 500 events, served to new WS connections)

---

## Phase 9 — Gateway Server

### 9.1 HTTP Server (`gateway/`)
- [x] 🔴 `server.go` — `gin` router setup, CORS, recovery middleware
- [x] 🔴 `server.go` — Mount all REST routes (see SPEC §8.1)
- [x] 🔴 `server.go` — Mount WebSocket at `/ws`
- [x] 🔴 `server.go` — Serve frontend static files from `/frontend/dist`

### 9.2 REST Handlers
- [x] 🔴 `POST /network/start` — initialize orchestrator with given config
- [x] 🔴 `POST /network/reset` — teardown + reinit
- [x] 🔴 `GET  /network/state` — return full `NetworkState` JSON
- [x] 🔴 `GET  /network/config` — return current `Config` JSON
- [x] 🔴 `PUT  /network/config` — update runtime config (W/R/N/delays)
- [x] 🔴 `POST /nodes` — spawn node
- [x] 🔴 `DELETE /nodes/:id` — graceful leave
- [x] 🔴 `POST /nodes/:id/crash` — crash node
- [x] 🔴 `GET  /nodes/:id` — node state snapshot
- [x] 🔴 `POST /kv` — insert key-value (quorum write)
- [x] 🔴 `GET  /kv/:key` — read key (quorum read)
- [x] 🔴 `GET  /kv/:key/replicas` — show all replicas + their vector clocks
- [x] 🔴 `POST /lookup` — trace lookup path
- [x] 🟡 `POST /scenarios/:name/run` — run named scenario
- [x] 🟡 `GET  /metrics` — Prometheus text format

### 9.3 WebSocket Hub
- [x] 🔴 `websocket.go` — `Hub` struct managing connected clients
- [x] 🔴 `websocket.go` — `HandleUpgrade(w, r)` — WebSocket upgrade
- [x] 🔴 `websocket.go` — Parse client subscription request (`subscribe` message)
- [x] 🔴 `websocket.go` — Fan-out events from `EventBus` to subscribed clients
- [x] 🔴 `websocket.go` — Send `ring_state` snapshot on initial connect
- [x] 🔴 `websocket.go` — Handle client disconnect gracefully

---

## Phase 10 — Frontend Implementation

### 10.1 Foundation
- [x] 🔴 `types/dht.ts` — TypeScript types for all API responses + WS events
- [x] 🔴 `api/client.ts` — `fetch`-based REST client functions (typed)
- [x] 🔴 `hooks/useWebSocket.ts` — auto-reconnect WebSocket hook
- [x] 🔴 `hooks/useRingState.ts` — maintain live ring state from WS events + REST polling
- [x] 🔴 `store/dhtStore.ts` — Zustand store: nodes, keys, config, events, selected node

### 10.2 Ring Visualizer
- [x] 🔴 `RingCanvas.tsx` — D3 SVG: draw ring circle, position nodes by ID hash
- [x] 🔴 `RingCanvas.tsx` — Render node circles with health-based coloring
- [x] 🔴 `RingCanvas.tsx` — Render key dots on ring circumference
- [x] 🔴 `NodeMarker.tsx` — Hover tooltip with ID, successor, predecessor, key count
- [x] 🔴 `NodeMarker.tsx` — Click → navigate to `/nodes/:id`
- [x] 🔴 `RingCanvas.tsx` — Render successor arcs (solid) + predecessor arcs (dashed)
- [x] 🟡 `FingerRays.tsx` — Toggle-able finger table rays per node
- [x] 🟡 `LookupAnimation.tsx` — Animated pulse along arc path for each lookup hop
- [x] 🟡 `LookupAnimation.tsx` — Glowing trail on traversed arcs, hop count badge
- [x] 🟡 `KeyDots.tsx` — Click key dot → highlight replica nodes

### 10.3 Controls Sidebar
- [x] 🔴 Protocol selector: Chord | Kademlia (calls `PUT /network/config`)
- [x] 🔴 [+ Add Node] button → `POST /nodes`, animate join
- [x] 🔴 [Kill Node] button (selected node context)
- [x] 🔴 [Crash Node] button (selected node context)
- [x] 🔴 Insert Key form: key + value → `POST /kv`
- [x] 🔴 Read Key form: key → `POST /lookup` → show trace
- [x] 🟡 Quorum sliders: W, R, N (0..5 range)
- [x] 🟡 Stabilization interval slider
- [x] 🟡 Simulated delay slider (0–500ms per hop)
- [x] 🟡 Packet loss rate slider (0–50%)

### 10.4 Node Inspector
- [x] 🔴 Header: ID, addr, status badge (healthy/stabilizing/unreachable)
- [x] 🔴 Successor / predecessor display (clickable → navigate)
- [x] 🔴 SuccessorList display (Chord only)
- [x] 🟡 Finger table: first 16 entries shown
- [x] 🟡 K-bucket tree (Kademlia only): 160 buckets, lazy-render
- [x] 🔴 Owned keys table: key_hex | value_preview | vector_clock | TTL
- [x] 🟡 Live event log for this node (filter WS stream by nodeID)

### 10.5 Lookup Tracer
- [x] 🔴 Key input with live SHA-1 hash preview
- [x] 🔴 [Trace Lookup] → `POST /lookup` → display result
- [x] 🔴 Hop list: fromNode | toNode | mechanism (fingerTable[i] / successor / k-bucket)
- [x] 🟡 Mini ring diagram with only involved nodes + animated path
- [x] 🟡 Chord vs Kademlia comparison panel: side-by-side hops for same key
- [x] 🟡 Lookup history: last 20 traces with hop counts

### 10.6 Consistency Dashboard
- [x] 🟡 Per-key replication status table: key | intended N | actual replicas | status
- [x] 🟡 Expand row: each replica node, value preview, vector clock display
- [x] 🟡 Vector clock DAG: D3 force graph showing causal history per key
- [x] 🟡 Conflict log: list of detected + resolved conflicts with resolution strategy shown
- [x] 🟡 Anti-entropy log: live feed of gossip sync events
- [x] 🟡 Convergence meter: % replicas consistent after write, time-series chart

### 10.7 Metrics Panel
- [x] 🟡 Recharts time-series: lookup hop count over time (rolling 60s window)
- [x] 🟡 Histogram: hop count distribution (bucket chart)
- [x] 🟡 Node count gauge + key count gauge
- [x] 🟡 Stabilization cycles per second
- [x] 🟡 Replication lag: time between write and full replication

### 10.8 Scenario Runner
- [x] 🟡 List of scenarios with description cards
- [x] 🟡 [Run] button → `POST /scenarios/:name/run` + subscribe to events
- [x] 🟡 Step-by-step display of scenario progress
- [x] 🟡 Result summary: convergence time, hop counts, data loss count

---

## Phase 11 — Integration & Polish

### 11.1 End-to-End Integration Tests
- [x] 🔴 Start gateway → add 10 Chord nodes → insert 100 keys → verify all readable
- [x] 🔴 Start gateway → add 10 Kademlia nodes → insert 100 keys → verify all readable
- [x] 🔴 Kill 3 nodes (30% churn) → verify all keys still readable with R=2, N=3
- [x] 🔴 Network partition → write to both halves → heal → gossip → verify convergence
- [x] 🔴 WebSocket: subscribe → spawn node → verify `node_join` event received on WS
- [x] 🔴 Lookup trace: verify hop count in response matches actual path

### 11.2 Performance Validation
- [x] 🟡 Benchmark: 100-node Chord ring, 1000 lookups → hops fit O(log N) ± 20%
- [x] 🟡 Benchmark: 100-node Kademlia, 1000 lookups → hops fit O(log N) ± 20%
- [x] 🟡 Verify: ring stabilizes (all successors correct) within 3s after 10-node bootstrap
- [x] 🟡 Verify: key migration completes within 500ms for 10k keys on node join

### 11.3 Frontend Polish
- [x] 🟡 Loading states for all async operations
- [x] 🟡 Error toasts for failed operations (quorum failure, node unreachable)
- [x] 🟡 Responsive layout: works at 1280px wide minimum
- [x] 🟡 Dark mode support (Tailwind `dark:` classes)
- [x] 🟢 Keyboard shortcuts: `N` = add node, `K` = insert key, `L` = lookup
- [x] 🟢 Tutorial overlay: step-by-step guided tour of UI (first-time user)

### 11.4 Documentation
- [x] 🔴 `README.md` — setup instructions, screenshots, quick start
- [x] 🟡 Inline code comments on all non-trivial algorithms
- [x] 🟡 `go doc` style godoc comments on all exported types/functions
- [x] 🟢 Architecture decision records (ADR) in `docs/adr/`

---

## Phase 12 — Stretch Goals

- [x] 🔵 S/Kademlia: proof-of-work node ID generation (SHA-256 prefix matching)
- [x] 🔵 CRDT values: G-Counter and OR-Set per key, merge on conflict instead of LWW
- [x] 🔵 Bidirectional Chord: anti-finger table for counter-clockwise lookup acceleration
- [x] 🔵 BadgerDB persistence: nodes survive restart with full state reload
- [x] 🔵 Multi-process mode: launch nodes as separate OS processes over real TCP
- [x] 🔵 Latency topology simulation: simulate geographic distance via per-link delay matrix
- [x] 🔵 Chord: `O(log²N)` finger table initialization (reuse existing entries)
- [x] 🔵 Kademlia: value caching on closer nodes during lookup (`FindValue` shortcut)
- [x] 🔵 Prometheus + Grafana dashboard via docker-compose
- [x] 🔵 Full OpenAPI 3.0 spec for gateway (auto-generated from code)

---

## Summary Progress Tracker

> Last updated: 2026-03-09 — 286/286 items complete (100%). All Go tests pass. TypeScript build clean.
> Frontend enhancements: Barlow Condensed + JetBrains Mono typography, dot-grid background, heat-fill ring nodes, animated connection status, redesigned header with ring logo, full 160-entry finger table, fixed replication_lag event type gap.

| Phase | Tasks | Done | Status |
|-------|-------|------|--------|
| 0. Bootstrap | 25 | 25 | ✅ Complete |
| 1. Consistent Hashing | 17 | 17 | ✅ Complete |
| 2. Transport (in-process + TCP) | 13 | 13 | ✅ Complete |
| 3. KV Store + CRDT | 18 | 18 | ✅ Complete |
| 4. Chord Protocol | 37 | 37 | ✅ Complete |
| 5. Kademlia Protocol | 31 | 31 | ✅ Complete |
| 6. Replication + Quorum | 17 | 17 | ✅ Complete |
| 7. Simulation + Scenarios | 14 | 14 | ✅ Complete |
| 8. Event Bus | 6 | 6 | ✅ Complete |
| 9. Gateway (REST + WS + Metrics) | 25 | 25 | ✅ Complete |
| 10. Frontend | 53 | 53 | ✅ Complete |
| 11. Integration + Performance | 20 | 20 | ✅ Complete |
| 12. Stretch Goals | 10 | 10 | ✅ Complete |
| **Total** | **286** | **286** | **100% ✅** |
| **TOTAL** | **230** | **~140 / 230 done** |
