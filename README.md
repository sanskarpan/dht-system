# DHT System

A production-grade Distributed Hash Table implementation in Go + React, featuring both **Chord** and **Kademlia** protocols with a live visual simulator.

## Overview

This project implements two classic DHT protocols from scratch and exposes them through an interactive web UI. You can spawn nodes, insert keys, trace lookups hop-by-hop, simulate node failures and network partitions — all in real time.

```
┌─────────────────────────────────────────────────────────┐
│                      Browser (React)                     │
│  Ring Visualizer · Lookup Tracer · Node Inspector        │
│  Consistency Dashboard · Metrics · Scenario Runner       │
└────────────────────┬────────────────────────────────────┘
                     │  HTTP REST + WebSocket
┌────────────────────▼────────────────────────────────────┐
│               Gateway (Gin, port 8080)                   │
│  /network/* · /nodes/* · /kv/* · /lookup · /ws          │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│              Simulation Orchestrator                      │
│  Chord or Kademlia nodes · Quorum replication            │
│  In-process transport · Fault injection                  │
└─────────────────────────────────────────────────────────┘
```

## Features

- **Two DHT protocols** — Chord (ring + finger table) and Kademlia (XOR metric + k-buckets)
- **Quorum replication** — configurable N/W/R (default 3/2/2) with vector-clock conflict resolution
- **Live visualization** — D3 ring diagram with node positions, key dots, successor arcs, and finger rays
- **Hop-by-hop lookup tracing** — see every node visited during a lookup
- **Fault injection** — kill nodes, crash nodes, simulate packet loss, inject latency
- **Network partitions** — split the ring into groups, observe divergence, heal and watch recovery
- **Real-time events** — WebSocket stream of all DHT events (join, leave, stabilize, lookup, quorum, etc.)
- **Scenario runner** — pre-built scenarios: bootstrap, churn, partition, hot-key, benchmark

## Quick Start

### Prerequisites

| Tool | Version |
|------|---------|
| Go | 1.21+ |
| Node.js | 18+ |
| npm | 9+ |

### 1. Clone and enter the project

```bash
git clone https://github.com/sanskarpan/dht-system.git
cd dht-system/dht-system
```

### 2. Install Go dependencies

```bash
go mod download
```

### 3. Build and run the gateway

```bash
make build
./bin/gateway
# → DHT System started: protocol=chord, nodes=6
# → Server listening on :8080
```

Or with environment variables:

```bash
PROTOCOL=kademlia INITIAL_NODES=10 PORT=9000 ./bin/gateway
```

### 4. Build the frontend

In a separate terminal:

```bash
cd frontend
npm install
npm run build
```

The gateway serves the built frontend from `/frontend/dist` at `http://localhost:8080`.

### 5. (Optional) Frontend dev server

For hot-reload during frontend development:

```bash
cd frontend
npm run dev    # starts at http://localhost:5173
```

The Vite dev server proxies API calls to the gateway at `localhost:8080`.

## Usage

### Web UI

Open `http://localhost:8080` in your browser.

**Ring Visualizer** (home) — The main view. Nodes appear on the ring at positions determined by their SHA-1 ID. Keys appear as dots. Click a node to inspect it. Use the sidebar to:
- Switch protocols (Chord ↔ Kademlia)
- Add / kill / crash nodes
- Insert and read keys
- Run named scenarios

**Lookup Tracer** (`/lookup`) — Enter a key and click **Trace Lookup** to see each hop, the mechanism used (finger table, successor, k-bucket), and total latency.

**Node Inspector** (`/nodes/:id`) — Per-node view: successor/predecessor, successor list, finger table (first 16 entries), owned keys with vector clocks and TTL.

**Consistency Dashboard** (`/consistency`) — Replication status per key, anti-entropy event log.

**Metrics** (`/metrics-ui`) — Live charts: lookup hop count over time, node/key gauges.

**Scenario Runner** (`/scenarios`) — Run bootstrap, churn, partition, hot-key, or benchmark scenarios.

### REST API

All endpoints speak JSON.

```
POST /api/v1/network/start        Start or replace the active network
POST /api/v1/network/reset        Reset to an empty network
GET  /api/v1/network/state        Full network snapshot
GET  /api/v1/network/config       Current DHT config
PUT  /api/v1/network/config       Update W/R/N/delays at runtime
POST /api/v1/network/partition    Split nodes into partition groups
POST /api/v1/network/heal         Remove all partitions
GET  /api/v1/network/faults       Inspect active partitions / link latency
PUT  /api/v1/network/links        Set per-link latency
DELETE /api/v1/network/links      Clear all per-link latency

POST /api/v1/nodes                Spawn a new node
DELETE /api/v1/nodes/:id          Graceful leave
POST /api/v1/nodes/:id/crash      Abrupt crash
GET  /api/v1/nodes/:id            Node state snapshot

POST /api/v1/kv                   Insert key-value (quorum write)
GET  /api/v1/kv/:key              Read key (quorum read)
GET  /api/v1/kv/:key/replicas     All replicas + vector clocks

POST /api/v1/lookup               Traced lookup → hop list

POST /api/v1/scenarios/:name/run  Run a named scenario
```

**Example — insert and read a key:**

```bash
# Start with 10 nodes
curl -s -X POST http://localhost:8080/api/v1/network/start \
  -H 'Content-Type: application/json' \
  -d '{"protocol":"chord","nodeCount":10}'

# Insert
curl -s -X POST http://localhost:8080/api/v1/kv \
  -H 'Content-Type: application/json' \
  -d '{"key":"hello","value":"world"}'

# Read
curl -s http://localhost:8080/api/v1/kv/hello

# Trace the lookup path
curl -s -X POST http://localhost:8080/api/v1/lookup \
  -H 'Content-Type: application/json' \
  -d '{"key":"hello"}' | jq .
```

### WebSocket Events

Connect to `ws://localhost:8080/ws`. Send a subscription message:

```json
{"action":"subscribe","events":["node_join","node_leave","lookup_hop","write_quorum","read_quorum","stabilize","gossip_sync","bucket_update"]}
```

Receive events in real time:

```json
{"type":"node_join","payload":{"nodeId":"a3f2...","addr":"127.0.0.1:7003","successor":"7c91..."}}
{"type":"lookup_hop","payload":{"fromNode":"a3f2...","toNode":"7c91...","mechanism":"fingerTable[4]","hopIndex":2}}
{"type":"write_quorum","payload":{"key":"hello","acksReceived":2,"w":2,"n":3,"success":true,"lagMs":4}}
```

## Configuration

Edit `config.yaml` (or set env vars) before starting the gateway:

```yaml
server:
  port: 8080
  cors_origins: ["http://localhost:5173"]

dht:
  protocol: chord        # chord | kademlia
  replication_n: 3       # N: replicate to N nodes
  write_quorum: 2        # W: wait for W acks
  read_quorum: 2         # R: collect R responses

chord:
  stabilize_interval: 500ms
  fix_fingers_interval: 1s
  successor_list_size: 8

kademlia:
  k: 20                  # bucket size / replication factor
  alpha: 3               # parallel RPC concurrency
  republish_interval: 24s
  expire_ttl: 25s

simulation:
  initial_nodes: 6
  sim_delay: 0ms         # per-hop latency injection
  sim_loss_rate: 0.0     # packet loss rate [0.0–1.0]
```

### Gateway Security Controls

Mutating routes can be protected without affecting local development defaults.

```bash
GATEWAY_API_TOKEN=change-me \
GATEWAY_MUTATION_LIMIT_PER_MINUTE=60 \
./bin/gateway
```

- `GATEWAY_API_TOKEN` enables API-key protection for mutating routes.
- `GATEWAY_MUTATION_LIMIT_PER_MINUTE` applies a per-IP fixed-window limit to mutating routes.
- Send the token as either `X-API-Key: ...` or `Authorization: Bearer ...`.
- Read-only routes remain open so dashboards and health checks continue to work by default.

## Architecture

### Packages

```
internal/
├── consistent/      SHA-1 ring arithmetic, virtual nodes, IDToHex/IDToFloat
├── transport/       Transport interface, InProcessTransport, FaultInjectingTransport
├── store/           KVStore (RWMutex + TTL eviction), ValueEntry
├── replication/     QuorumManager (N/W/R), VectorClock, conflict resolver (LWW)
├── chord/           ChordNode: finger table, stabilize, join/leave, RPC handlers
├── kademlia/        KademliaNode: routing table (160 k-buckets), iterative lookup, join
├── events/          EventBus (pub/sub, history ring buffer), all event types
└── simulation/      Orchestrator: node lifecycle, quorum routing, partition control
gateway/
├── server.go        Gin router, CORS, static file serving
├── rest.go          All REST handlers
└── websocket.go     WebSocket hub, event fan-out
frontend/
└── src/
    ├── components/  RingVisualizer, LookupTracer, NodeInspector, ConsistencyDash, …
    ├── hooks/       useWebSocket, useRingState
    ├── store/       Zustand DHT store
    └── api/         Typed fetch client
```

### Chord Protocol

Chord organizes nodes on a 160-bit circular ID space. Each node maintains:
- **Successor** — next node clockwise
- **Predecessor** — previous node clockwise
- **Finger table** — 160 shortcuts: `finger[i]` = successor of `(self + 2^i) mod 2^160`
- **Successor list** — 8 backup successors for fault tolerance

Lookup runs in **O(log N)** hops: at each step, forward to the closest preceding finger. Stabilization runs every 500ms to repair the ring after joins and crashes.

### Kademlia Protocol

Kademlia uses XOR distance. Each node maintains a routing table of 160 k-buckets (k=20). Bucket `i` holds contacts at XOR distance in `[2^i, 2^(i+1))`.

Iterative lookup sends α=3 parallel `FIND_NODE` RPCs per round, merging results and re-sorting by XOR distance until the k closest nodes are fully queried. Typical convergence: **O(log N)** rounds.

### Replication

Both protocols use the same quorum layer:
- **Write**: fan-out to N ring-successor nodes, wait for W acks. Fail if fewer than W respond.
- **Read**: fan-out to N nodes, collect R responses. Apply vector-clock comparison + LWW conflict resolution.
- **Conflict**: if two entries are concurrent (neither causally dominates), last-write-wins by wall-clock timestamp.

## Development

```bash
# Run all tests
make test

# Run tests with verbose output
make test-verbose

# Run a specific package
go test -v -race ./internal/kademlia/...
go test -v -race ./internal/simulation/...

# Lint (requires golangci-lint)
make lint

# Regenerate protobuf stubs
make proto-gen

# Frontend dev mode
make frontend-dev
```

### Test Coverage

| Package | Tests |
|---------|-------|
| `consistent` | Hash uniformity (χ²), wrap-around interval arithmetic, ring arithmetic |
| `chord` | 8-node ring lookup, hop bounds, join/leave key migration, crash recovery, concurrent joins |
| `kademlia` | XOR triangle inequality, bucket index, k-bucket LRU, iterative lookup convergence, store+retrieve, concurrent lookup |
| `replication` | Vector clock ordering, LWW resolution, quorum write/read |
| `simulation` | 10-node Chord: 100 keys readable; 10-node Kademlia: 100 keys readable; 30% churn resilience; partition+heal; WebSocket events; lookup hop count |

## Protocol Comparison

| Feature | Chord | Kademlia |
|---------|-------|----------|
| ID space | 160-bit ring | 160-bit XOR space |
| Routing table | Finger table (160 entries) | 160 k-buckets (k=20) |
| Lookup hops | O(log N) | O(log N) |
| Parallel RPCs | No | Yes (α=3 per round) |
| Join complexity | O(log² N) | O(log N · log N) |
| Successor list | Yes (8 backups) | N/A (k-bucket redundancy) |
| Key republication | Manual (migrate on join) | Background every 24s |

## Fault Tolerance

With default settings (N=3, W=2, R=2):
- **Node failure**: up to N−W = 1 replica node can fail without losing write ability; up to N−R = 1 can fail without losing read ability
- **30% churn**: 3/10 nodes killed → all keys remain readable (tested in integration tests)
- **Network partition**: keys written before partition remain readable from within each partition; after healing, stabilization restores full availability
