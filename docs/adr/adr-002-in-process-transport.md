# ADR 002: In-Process Transport for Inter-Node Communication

## Status

Accepted

## Context

A DHT simulation requires nodes to send messages to one another: lookup RPCs, store requests, stabilization pings, and gossip messages. In a production DHT (e.g., BitTorrent's Mainline DHT or Ethereum's discovery layer), these messages travel over UDP or TCP and are serialized to a wire format (e.g., bencode, Protobuf).

For this system, the primary use case is **educational visualization**, not production deployment or realistic performance benchmarking. Key concerns include:

- **Observability:** Every message exchange must be interceptable so the frontend can animate routing paths and display per-hop latency.
- **Determinism:** Network jitter and OS scheduling should not affect which routing paths are taken, so algorithm behavior can be reliably demonstrated.
- **Operational simplicity:** Running the simulation should require no port allocation, firewall configuration, or external services.
- **Extensibility:** A future mode for realistic network benchmarking should not require rewriting the protocol implementations.

Real TCP or gRPC transport would introduce OS-level concurrency, port management overhead, serialization costs, and non-deterministic timing — all of which obscure the algorithmic behavior being taught.

## Decision

All inter-node communication during simulation will use **InProcessTransport**: direct in-process function calls between `Node` instances, dispatched through a central `TransportRegistry`. No sockets, no serialization.

The design is:

- Each `Node` registers itself with the `TransportRegistry` on creation using its node ID.
- When `Node A` wants to send a message to `Node B`, it calls `TransportRegistry.send(targetId, message)`. The registry resolves `targetId` to the live `Node B` object and invokes its message handler directly.
- **Simulated delay** is injected at the registry layer via a configurable `perHopDelayMs` parameter. The registry wraps each dispatch in a `setTimeout` (or equivalent async delay) before invoking the handler, making routing animations temporally meaningful.
- **Simulated packet loss** can be injected via a configurable `dropRate` probability at the registry layer, allowing fault-tolerance scenarios to be demonstrated.
- A `GrpcTransport` stub class is defined in the codebase with the same interface as `InProcessTransport` but is not wired up by default. It documents the extension point for future realistic network mode.

The transport interface is:

```
interface Transport {
  send(targetId: NodeId, message: Message): Promise<Response>
  register(nodeId: NodeId, handler: MessageHandler): void
  deregister(nodeId: NodeId): void
}
```

Both `InProcessTransport` and the `GrpcTransport` stub implement this interface.

## Consequences

**Positive:**

- Zero port management: the entire simulation runs as a single process with no external dependencies.
- Fully deterministic routing behavior when `perHopDelayMs` is set to 0; the same lookup always traverses the same nodes in the same order.
- Every message exchange is interceptable at the registry layer, making it straightforward to emit WebSocket events for the frontend visualizer without modifying protocol code.
- Simulated delay and drop rate give instructors control over demonstrating latency-sensitive and fault-tolerance behaviors without a real network.
- No serialization/deserialization cost keeps simulation throughput high for large node counts.

**Negative:**

- Realistic network benchmarking is not possible. Throughput numbers, latency distributions, and bandwidth consumption figures produced by this system do not reflect real-world DHT performance.
- All nodes share the same memory space and process heap. A bug that corrupts shared state (e.g., mutating a message object after sending) can produce behavior that would never occur in a real distributed system.
- The `GrpcTransport` stub is not tested or maintained; significant effort would be required to make it production-ready.

**Neutral:**

- The configurable `perHopDelayMs` is set to a default of 50ms in the development configuration, giving the frontend visualizer enough time to animate each hop. This value has no relationship to any real network characteristic.
- Simulated packet loss via `dropRate` exercises protocol retry logic (e.g., Kademlia's alpha-parallel lookups) but does not model real network conditions such as congestion or asymmetric loss.
