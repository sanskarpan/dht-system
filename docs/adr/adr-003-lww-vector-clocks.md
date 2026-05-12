# ADR 003: Last-Write-Wins with Vector Clocks for Conflict Resolution

## Status

Accepted

## Context

In a DHT without coordination (no leader, no two-phase commit), concurrent writes to the same key from different nodes produce **conflicting versions**. The system must decide:

1. How to **detect** that two versions are in conflict vs. causally ordered.
2. How to **resolve** conflicts when they are detected.

Several strategies exist with different trade-offs:

| Strategy | Detection | Resolution | Data safety |
|---|---|---|---|
| Last-Write-Wins (LWW) by timestamp | None | Most recent timestamp wins | Lossy — concurrent writes can be silently dropped |
| Vector clocks | Causal ordering | Expose siblings to application | Lossless — application decides |
| CRDTs (e.g., G-Counter, OR-Set) | Not needed | Mathematically merge-safe | Lossless — merge always correct |
| Paxos / Raft | Consensus | Quorum decides | Lossless — requires coordination |

The system's primary goal is **educational visualization of DHT mechanics**, not providing a production-grade storage engine. Conflict resolution behavior must be:

- Easy to explain and visualize in the frontend.
- Distinguishable from causal ordering (so students can see the difference between "A happened before B" and "A and B are concurrent").
- Implementable without consensus protocols, which would add coordination overhead that obscures DHT routing behavior.

## Decision

The system will use **Last-Write-Wins (LWW) by wall-clock timestamp** as the primary conflict resolution strategy, combined with **vector clocks** for conflict detection.

The design is as follows:

- Every value stored in the DHT is wrapped in a `VersionedValue` envelope:
  ```
  {
    value: any,
    timestamp: number,       // Unix ms at write time (from writing node's clock)
    vectorClock: VectorClock // { [nodeId: string]: number }
  }
  ```
- When a node receives a write for a key it already holds, it compares vector clocks:
  - If the incoming version **dominates** (all components >= local, at least one strictly greater): accept the incoming version.
  - If the local version **dominates**: discard the incoming version.
  - If the versions are **concurrent** (neither dominates): apply LWW — the version with the higher `timestamp` wins. The losing version's value is discarded but logged to the **conflict history**.
- The conflict history is an append-only in-memory log per node, surfaced via the REST API (`GET /nodes/:id/conflicts`) and streamed to the frontend via WebSocket events of type `CONFLICT_DETECTED`.
- Vector clock increments follow standard rules: a node increments its own component on every write before propagating.

**CRDT-based merge (G-Counter, OR-Set)** is acknowledged as a stretch goal and noted in the codebase with `// TODO(crdt):` comments at the relevant merge sites. No CRDT implementation is included in the current scope.

## Consequences

**Positive:**

- LWW is the simplest possible conflict resolution policy to explain and animate. Students can observe the timestamp comparison and understand immediately why one value wins.
- Vector clocks add pedagogical value by making the distinction between causal ordering and true concurrency visible — a core concept in distributed systems education (Lamport, 1978).
- Conflict history logging means no write is silently lost without a trace; instructors can demonstrate conflict scenarios and then inspect what was discarded.
- No coordination overhead: conflict resolution is entirely local to the receiving node.

**Negative:**

- LWW is **not safe** for true concurrent writes: the losing value is permanently discarded. In a production system this would be unacceptable without application-level merge logic.
- Wall-clock timestamps are unreliable in real distributed systems (clock skew, NTP drift). In this simulation, all nodes run in the same process with the same clock, so skew is negligible — but students must be warned that this does not reflect real-world conditions.
- Vector clocks grow unboundedly as the number of nodes increases. No vector clock pruning or truncation strategy is implemented.

**Neutral:**

- The `VectorClock` type is implemented as a plain `Record<string, number>` (map from node ID to logical counter). Comparison is O(N) in the number of nodes.
- The conflict history is in-memory only and is lost on process restart. Persistence of conflict history is not a goal of this system.
- The stretch goal of CRDT-based merge (G-Counter for counters, OR-Set for sets) would replace only the merge function at the conflict site and would not require changes to the vector clock detection layer.
