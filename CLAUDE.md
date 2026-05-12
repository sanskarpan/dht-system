# DHT System — Claude Session Memory

## Project Overview
Production-grade DHT system implementing both Chord and Kademlia protocols.
- **Backend:** Go 1.22+ in `/Users/sanskar/dev/Research/Projects/DHT-System/dht-system/`
- **Frontend:** React 18 + TypeScript + Vite in `dht-system/frontend/`
- **Spec:** SPEC.md · **Tracker:** CHECKLIST.md

## Current Phase Progress

| Phase | Status | Notes |
|-------|--------|-------|
| 0. Bootstrap | ⬜ Not started | |
| 1. Consistent Hashing | ⬜ Not started | |
| 2. Transport | ⬜ Not started | |
| 3. KV Store | ⬜ Not started | |
| 4. Chord Protocol | ⬜ Not started | |
| 5. Kademlia Protocol | ⬜ Not started | |
| 6. Replication | ⬜ Not started | |
| 7. Simulation | ⬜ Not started | |
| 8. Event Bus | ⬜ Not started | |
| 9. Gateway | ⬜ Not started | |
| 10. Frontend | ⬜ Not started | |
| 11. Integration | ⬜ Not started | |

## Key Architectural Decisions
- Go module: `github.com/sanskarpan/dht-system/dht-system`
- In-process transport for simulation (no real TCP for v1.0)
- Both Chord and Kademlia implement same `Node` interface for orchestrator
- All tunable values via `Config` struct — no magic numbers
- Event bus: single shared bus → WebSocket hub → clients

## Critical Correctness Notes
- Chord `IsInInterval`: handle ring wrap-around (start > end case)
- Kademlia `BucketIndex`: `159 - clz(XOR)` where clz = 160 - BitLen(xor)
- Vector clock `Compare`: all components dominate for After/Before
- `FindSuccessor` max hops guard: 3*m=480 hops max

## File Structure (inside dht-system/)
```
cmd/gateway/main.go   — HTTP/WS gateway entry point
cmd/node/main.go      — standalone node (TCP mode)
internal/consistent/  — hash ring, vnodes
internal/chord/       — Chord protocol
internal/kademlia/    — Kademlia protocol
internal/replication/ — quorum, vector clocks
internal/store/       — KV store, Merkle tree
internal/gossip/      — anti-entropy
internal/transport/   — in-process + TCP transport
internal/simulation/  — orchestrator, scenarios
internal/events/      — event bus
gateway/              — REST handlers, WebSocket hub
proto/                — .proto files
frontend/             — React app
```

## Session Log
- Session 1: Read SPEC.md + CHECKLIST.md. Starting Phase 0.
