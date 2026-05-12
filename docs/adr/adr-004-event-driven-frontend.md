# ADR 004: Zustand + WebSocket Event Stream with React Query for REST Polling

## Status

Accepted

## Context

The DHT simulation backend emits a continuous stream of asynchronous state changes:

- **Node join/leave events:** triggered by user actions or scheduled churn scenarios.
- **Stabilization rounds:** Chord's ring repair and Kademlia's bucket refresh run on configurable intervals.
- **Routing animations:** each hop in a lookup must be animated in sequence.
- **Gossip propagation:** replication writes fan out across multiple nodes asynchronously.
- **Conflict detection events:** produced when concurrent writes collide (see ADR 003).

The frontend must reflect all of these changes in near-real-time for the visualization to be educationally useful. The main options considered were:

| Approach | Latency | Complexity | State management |
|---|---|---|---|
| REST polling only | High (poll interval) | Low | Client re-fetches full state |
| WebSocket push only | Low | Medium | Client must reconstruct full state from events |
| WebSocket push + REST for initial state | Low | Medium-high | Hybrid: events for deltas, REST for snapshots |
| Server-Sent Events (SSE) | Low | Low | Unidirectional only; cannot send commands |

REST polling alone introduces latency equal to the poll interval, which makes routing animations jerky and stabilization events visually delayed. SSE is unidirectional and cannot support the bidirectional command channel needed for user-initiated actions (e.g., triggering a lookup, adding a node). Pure WebSocket-only state management requires the client to reconstruct all state from an event log, which complicates initial page load and reconnection.

The frontend also needs to support user-initiated mutations (add node, remove node, store key, trigger lookup) with request/response semantics, which fits the REST model naturally.

## Decision

The frontend will use a **hybrid data architecture**:

1. **Zustand global store** as the single source of truth for all rendered DHT state. Components read from the store and do not fetch data directly.

2. **WebSocket event stream** (`ws://host/events`) as the primary real-time data source. The backend pushes typed events to all connected clients. A central WebSocket manager subscribes to the stream and applies each event as a patch to the Zustand store via named actions (e.g., `useStore.getState().applyNodeJoin(event)`).

3. **React Query** for:
   - Initial state hydration on page load (`GET /simulation/state` — returns a full snapshot of all nodes, routing tables, and stored keys).
   - User-initiated mutations (`POST /nodes`, `DELETE /nodes/:id`, `POST /lookup`, `POST /store`) with optimistic update support.
   - Periodic fallback polling (`refetchInterval: 10000`) as a safety net if the WebSocket connection is degraded.

The event type taxonomy on the WebSocket stream includes:

```
NODE_JOINED | NODE_LEFT | LOOKUP_HOP | LOOKUP_COMPLETE |
STORE_WRITE | REPLICATION_WRITE | STABILIZE_ROUND |
CONFLICT_DETECTED | GOSSIP_PROPAGATED | SIMULATION_RESET
```

Each event payload includes a `protocol` field (`"chord"` | `"kademlia"`) and a `timestamp` field for ordering.

**Reconnection behavior:** The WebSocket manager implements exponential backoff reconnection. On successful reconnection, it triggers a React Query refetch of `GET /simulation/state` to resync the Zustand store to the current backend state, discarding any intermediate events that may have been missed during the disconnection window.

**Event replay** (replaying the full event log from the backend after reconnection rather than fetching a snapshot) is acknowledged as a stretch goal. It is not implemented in the current scope.

## Consequences

**Positive:**

- WebSocket push delivers routing hop events with sub-10ms latency from the backend emit to the Zustand store update, enabling smooth frame-by-frame routing animations.
- Zustand's synchronous update model means React components re-render deterministically on each event without intermediate loading states.
- React Query handles cache invalidation, deduplication, and retry logic for REST calls, removing boilerplate from component code.
- The REST fallback polling (`refetchInterval: 10000`) prevents the UI from remaining stale indefinitely if WebSocket events are lost.
- Separation of concerns: WebSocket manager handles event application; React Query handles mutations; Zustand holds state. Each layer is independently testable.

**Negative:**

- Frontend state **can diverge from backend state** during a WebSocket disconnection. The reconnection resync mitigates this but introduces a visible "flash" as the store is overwritten with the snapshot.
- The hybrid architecture increases cognitive overhead for frontend contributors who must understand three data-flow patterns (Zustand, WebSocket, React Query) rather than one.
- Without event replay, any routing animation that was in progress during a disconnection is permanently lost and cannot be reconstructed from the reconnection snapshot.
- Optimistic updates in React Query can temporarily show incorrect state if a mutation is rejected by the backend, requiring careful rollback handling.

**Neutral:**

- The Zustand store is not persisted to `localStorage` or any other client-side storage. A browser refresh triggers a full re-hydration from `GET /simulation/state`.
- The WebSocket connection is a single shared connection per browser tab (not per component). The WebSocket manager is initialized once at application startup outside the React tree.
- Event replay from the backend is tracked as a stretch goal. Implementing it would require the backend to maintain an append-only event log and expose a `GET /events?since=<cursor>` endpoint, and the frontend to apply events in order on reconnection rather than fetching a full snapshot.
