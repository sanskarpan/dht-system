# ADR 001: Dual-Protocol DHT Implementation (Chord + Kademlia)

## Status

Accepted

## Context

The DHT System is designed as an educational research platform for comparing the two most widely studied Distributed Hash Table protocols in academic literature: **Chord** (Stoica et al., 2001) and **Kademlia** (Maymounkov & Mazières, 2002). Each protocol has distinct routing mechanics:

- **Chord** organizes nodes on a consistent-hash ring and routes via a finger table of size O(log N), with successor/predecessor pointers for stabilization.
- **Kademlia** organizes nodes in a binary trie using the XOR distance metric and routes via k-buckets (one bucket per bit-prefix distance from the local node).

A naive approach would implement each protocol in a separate, independent codebase. This makes direct comparison difficult and doubles the surface area of infrastructure (transport, storage, visualization bindings, gateway API).

The primary goal of this system is to enable **side-by-side behavioral comparison** of routing efficiency, replication behavior, and fault tolerance between the two protocols. Both protocols must be observable through the same frontend visualizer and addressable through the same gateway API.

## Decision

Both Chord and Kademlia will be implemented in the same codebase, sharing a common **Transport interface** abstraction. Each protocol provides a concrete implementation conforming to that interface. The gateway API and frontend visualizer operate against the Transport interface and are therefore protocol-agnostic.

Concretely:

- A shared `Node` abstraction (or interface) defines the contract: `lookup(key)`, `store(key, value)`, `join(bootstrapNode)`, `leave()`, and `getRoutingTable()`.
- `ChordNode` implements this contract using a ring topology and finger table.
- `KademliaNode` implements this contract using XOR-metric k-buckets.
- The simulation runtime can instantiate either a Chord network or a Kademlia network, or both simultaneously in separate simulation contexts.
- The frontend receives a `protocol` field in every event payload, allowing the visualizer to render protocol-specific structures (ring vs. trie) as appropriate.

## Consequences

**Positive:**

- The frontend visualizer and gateway REST/WebSocket API require no protocol-specific branching at the transport or API layer.
- Direct side-by-side comparison of routing hop counts, stabilization convergence, and replication fanout is possible within the same session.
- Shared infrastructure (in-process transport, storage layer, event emitter) reduces total code size compared to two independent systems.
- Encourages clean interface design: any future protocol (e.g., Pastry, Tapestry) can be added by implementing the same `Node` interface.

**Negative:**

- The shared `Node` interface must be expressive enough for both protocols, which can lead to interface bloat or leaky abstractions (e.g., Chord-specific `predecessor` pointer has no Kademlia equivalent).
- Maintaining two non-trivial routing algorithm implementations increases long-term maintenance burden.
- Protocol-specific visualizations (ring arcs for Chord, trie diagrams for Kademlia) require branching in the frontend rendering layer despite the shared backend interface.

**Neutral:**

- The in-process transport means both protocols run in the same memory space. This is not production-realistic but is acceptable for the educational simulation context (see ADR 002).
- No serialization cost is incurred for inter-node messages, which keeps simulation performance high and algorithmic behavior clearly observable.
