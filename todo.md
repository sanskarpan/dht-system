# DHT System — Complete Audit Todo List

All findings from the full codebase audit. Items are grouped by area.
Status: `[ ]` = pending, `[x]` = done.

---

## BACKEND — Stub / Incomplete Implementations

- [x] **B-01** `pullKeysInRange` in `internal/chord/join.go` is a complete no-op stub (all params `_`, returns nil) — key migration on Chord node join silently never happens. Implement actual RPC-based key transfer.
- [x] **B-02** `runFixFingers` goroutine in `internal/chord/stabilize.go` does nothing but wait on `stopCh`. The real fix-fingers work runs inside `runStabilize`'s `fingerTicker`. Remove the dead goroutine or consolidate.
- [x] **B-03** `resetNetwork` handler in `gateway/rest.go` has comment "Not fully implemented" and only returns current state. Implement actual network teardown and re-initialization.
- [x] **B-04** `deleteKey` handler in `gateway/rest.go` is hardcoded to return `{"success":true}` without deleting anything. Implement actual DHT key deletion across all replicas.
- [x] **B-05** `/health` endpoint is defined in `openapi.yaml` but missing entirely from `gateway/server.go` and `rest.go`. Add `GET /health → {"status":"ok"}`.
- [x] **B-06** `ScenarioBenchmark` in `internal/simulation/scenarios.go` is fully implemented but never listed in `gateway/rest.go`'s `listScenarios` or `runScenario` switch — unreachable from UI. Add it.

---

## BACKEND — Dead / Unused Code

- [x] **B-07** `fixFingers()` in `internal/chord/finger.go` is fully implemented but has zero callers anywhere in production code. Either call it from `runStabilize` or remove it.
- [x] **B-08** `IsInIntervalLeftClosed()` in `internal/consistent/hash.go` is defined but never called in production code. Remove it.
- [x] **B-09** `MineNodeID()` in `internal/consistent/hash.go` — S/Kademlia PoW exported but never wired into node creation. Either wire it (call during node spawn) or document it as intentionally disabled.
- [x] **B-10** `VNodeRing` struct and all its methods in `internal/consistent/vnode.go` — only used in tests, never referenced in any production path. Move to a test helper file or document as test-only.
- [x] **B-11** `loggingMiddleware()` in `gateway/middleware.go` is defined but never registered in `server.go`. Either register it with `r.Use(loggingMiddleware())` or remove it.
- [x] **B-12** `runFixFingers` goroutine is launched in the node start sequence but does nothing (see B-02). Dead goroutine consuming a slot.

---

## BACKEND — Silent Error Discards

- [x] **B-13** `json.Marshal(payload)` error silently discarded in `internal/events/types.go` `MakeEvent`. Add error return or log + return empty event.
- [x] **B-14** `json.Marshal(v)` error silently discarded in `internal/transport/tcp.go` `mustMarshal`. Rename to `marshalOrLog` and log the error.
- [x] **B-15** `json.Marshal(state)` error silently discarded twice in `gateway/websocket.go` (broadcast and initial send). Log the error and skip the send if marshal fails.
- [x] **B-16** `_ = c.ShouldBindJSON(&body)` in `gateway/rest.go` `spawnNode` ignores JSON parse error. Return `400 BadRequest` if bind fails.
- [x] **B-17** TCP server `_ = srv.Serve(ln)` in `internal/transport/tcp.go` discards the serve error. Log it and consider restarting or signaling shutdown.
- [x] **B-18** All 4 scenario goroutines in `gateway/rest.go` discard errors: `go func() { _ = h.orch.ScenarioX(...) }()`. Log errors via the event bus or zap.
- [x] **B-19** `_ = n.Transport.UpdateFingerTable(...)` and `_ = n.Transport.TransferKeys(...)` in `internal/chord/join.go` silently discard RPC errors. Log or propagate them.

---

## BACKEND — Algorithmic / Correctness Issues

- [x] **B-20** `Diff()` in `internal/store/merkle.go` is O(N) full scan instead of O(log N) tree descent. Fix it to actually descend the Merkle tree and return only the differing subtrees.
- [x] **B-21** `fromAddrs` slice in `internal/kademlia/lookup.go` is computed, immediately assigned to `_`, and discarded. Remove the dead computation.
- [x] **B-22** `findValue bool` parameter in `internal/kademlia/lookup.go` `iterativeLookup` is passed but never read inside the function. Either use it to short-circuit on value found or remove the parameter.
- [ ] **B-23** `IterativeFindValue` and `iterativeLookup` in `internal/kademlia/lookup.go` share ~70% code (sorted-contact management, alpha concurrency, closest-seen tracking). Refactor into one shared function with a value-return path.

---

## BACKEND — Data Model Gaps

- [x] **B-24** `KeyState` struct in `internal/simulation/orchestrator.go` is missing `ReplicaCount int` field. The frontend's `KeySnapshot.replicaCount` is always `undefined`/0 — replica health bars and consistency views are permanently broken. Add the field and populate it.
- [x] **B-25** Original key string is never stored in `ValueEntry` — only the SHA-1 hash is stored. `GetNetworkState()` sets `Key = keyHex`. Frontend displays unreadable 40-char hex hashes. Store the original key string in `ValueEntry` and surface it in `KeyState`.
- [x] **B-26** `getKeyReplicas` in `gateway/rest.go` returns only `{nodeId, value}` per replica. The frontend `ConsistencyDash` expects a full `ValueEntry` with `clock`, `timestamp`, `ttlNs`. Extend the response to include the full entry.

---

## BACKEND — Observability / Logging

- [x] **B-27** `DHTReadTotal` Prometheus counter in `gateway/metrics.go` is defined but never incremented. Call `.Inc()` in the `getKey` handler.
- [x] **B-28** `DHTStabilizeCycles` Prometheus counter in `gateway/metrics.go` is defined but never incremented. Call `.Inc()` in the stabilize event handler/broadcast.
- [x] **B-29** `DHTGossipSyncs` Prometheus counter in `gateway/metrics.go` is defined but never incremented. Call `.Inc()` in the gossip sync event handler.
- [x] **B-30** `fmt.Printf` used for startup messages in `cmd/gateway/main.go` instead of structured `zap` logger. Replace with `zap.L().Info(...)`.
- [x] **B-31** `fmt.Printf` used in `gateway/server.go` for startup banner instead of `zap`. Replace with `zap.L().Info(...)`.

---

## BACKEND — Configuration / Hardcoded Values

- [x] **B-32** All 4 scenario parameters in `gateway/rest.go` are hardcoded (10 nodes for bootstrap, 30s/6 nodes for churn, specific keys for partition, 1000 keys for hotkey). Accept optional params from request body.
- [x] **B-33** WebSocket ping ticker (30s) and read deadline (60s) hardcoded in `gateway/websocket.go`. Move to a config constant or environment variable.
- [x] **B-34** Ring state broadcast interval hardcoded to `5 * time.Second` in `gateway/websocket.go`. Move to config.
- [x] **B-35** `PORT` env var parse failure is silent in `cmd/gateway/main.go` — invalid value silently falls back to 8080 with no warning. Add `zap.L().Warn(...)` on parse error.

---

## BACKEND — Security (dev-acceptable, production gaps)

- [x] **B-36** CORS wildcard `Access-Control-Allow-Origin: *` in `gateway/middleware.go`. Document as dev-only and add a config flag to restrict in production.
- [x] **B-37** WebSocket `CheckOrigin` always returns `true` in `gateway/websocket.go`. Document as dev-only and add origin validation behind a config flag.

---

## BACKEND — Validation

- [x] **B-38** `spawnNode` in `gateway/rest.go` does not validate that `addr` is a valid address format. Add basic non-empty validation.
- [x] **B-39** `startNetwork` in `gateway/rest.go` silently replaces `nodeCount <= 0` with 6 instead of returning 400. Return `400 BadRequest` for invalid counts.
- [x] **B-40** Node ID path param in `gateway/rest.go` `getNode`/`deleteNode`/`crashNode` handlers has no length/format validation. Add hex format check.

---

## BACKEND — Missing Wiring

- [x] **B-41** CRDT `GCounter` and `ORSet` in `internal/crdt/` are implemented but never integrated into the main store or replication layer. Wire them or document as future extension.
- [x] **B-42** Anti-finger table (`internal/chord/anti_finger.go`) is built and populated but not actually used in routing decisions. Either use it in `FindSuccessor` or document as non-operational. *(Verified: anti-finger table IS used in lookup.go line 157.)*

---

## FRONTEND — Dead Code

- [x] **F-01** `src/App.css` is default Vite boilerplate (`.logo`, `.card`, logo-spin). Not imported anywhere. Delete it.
- [x] **F-02** Dead store actions in `store/dhtStore.ts`: `addNode`, `removeNode`, `updateNode`, `setProtocol` — zero callers. Remove them.
- [x] **F-03** Unused API exports in `api/client.ts`: `getNetworkConfig`, `startNetwork`, `resetNetwork`, `deleteKey`, `getMetricsJson` — zero callers in any component. Remove or wire them up.

---

## FRONTEND — API / Data Mismatches

- [x] **F-04** `getKeyReplicas` response type in `api/client.ts` declares `Array<{nodeId: string; entry: ValueEntry}>` but backend sends `Array<{nodeId: string; value: string}>`. Fix the type to match what the backend actually returns, and update `ConsistencyDash` to use it.
- [x] **F-05** `KeySnapshot.replicaCount` in `types/dht.ts` is always `undefined` because backend `KeyState` doesn't include it (see B-24). Once B-24 is fixed, verify the frontend correctly renders replica counts.
- [x] **F-06** Key names display as hex hashes in the UI because backend sends hash as key field (see B-25). Once B-25 is fixed, verify `ConsistencyDash` and `MetricsPanel` show readable key names.

---

## FRONTEND — State Management

- [x] **F-07** Dual state update path: `RingVisualizer` polls `GET /network/state` every 3s via TanStack Query AND WebSocket broadcasts `ring_state` every 5s. Both call `setNetworkState`. Consolidate to a single source of truth (prefer WebSocket, remove the REST poll or make it a fallback).

---

## FRONTEND — Missing UI for Backend Features

- [x] **F-08** `ScenarioBenchmark` added to backend (B-06) needs a corresponding entry in `ScenarioRunner`'s scenario list with description and run button. *(Rendered dynamically from API; no hardcoding needed.)*
- [x] **F-09** `deleteKey` API (B-04 when fixed) needs a Delete button in the key list or `ConsistencyDash`.
- [x] **F-10** `resetNetwork` API (B-03 when fixed) needs a Reset button somewhere in the UI (e.g., Scenarios page or a header action).

---

## CROSS-CUTTING

- [x] **X-01** `eslint-disable` comment in `frontend/src/components/RingVisualizer/Sidebar.tsx` line ~94. Investigate what rule it suppresses and fix the underlying issue instead.
- [x] **X-02** Cross-protocol RPC handlers (`chord/rpc_handler.go` and `kademlia/rpc_handler.go`) return errors when receiving calls meant for the other protocol — should panic/fail-fast with a clear message.
- [x] **X-03** Anti-finger table uses complex two's-complement index arithmetic without documentation — add inline comments explaining the algorithm (see B-42).
- [x] **X-04** OpenAPI spec `openapi.yaml` is out of sync: `getKeyReplicas` response schema shows `value: string` but frontend expects `ValueEntry`. Update the spec to match whichever side is fixed.
- [x] **X-05** Go tests for `consistent/hash.go` call `MineNodeID` — if PoW is intentionally disabled in production, mark those tests as skipped or move to a build tag. *(Already uses `testing.Short()` guard for expensive difficulty=16 test.)*
- [x] **X-06** Go module tidy — run `go mod tidy` to confirm all imports are clean and no unused deps linger after dead code removal.
- [x] **X-07** Frontend TypeScript strict check — run `tsc --noEmit` after all frontend changes to confirm zero type errors.
- [x] **X-08** Frontend production build check — run `npm run build` after all changes to confirm zero build errors and no bundle regressions.
- [x] **X-09** Go build check — run `go build ./...` from `dht-system/` after all backend changes to confirm zero compilation errors.
- [x] **X-10** Integration smoke test — start the full stack (`go run ./cmd/gateway` + `npm run dev`) and confirm: node spawn, key insert, lookup, replica view, and scenario run all work end-to-end.

---

## Summary

**60/60 items completed.**

Notable fixes along the way:
- **B-23**: Refactored into shared `iterativeSearch(roundFn)` in `internal/kademlia/lookup.go`
- **X-10**: Also fixed `runScenario` context bug — was using `c.Request.Context()` which cancelled on HTTP response; changed to `context.Background()` for detached background goroutines

---

## PROJECT HARDENING — Post-Audit Engineering Work

Status: `[ ]` = pending, `[x]` = done.

- [x] **P3-01** Enforce frontend quality gates in CI (`#129`) — add GitHub Actions coverage for frontend lint, unit tests, and Playwright E2E.
- [x] **P3-02** Eliminate GitHub Actions Node 20 deprecation warnings (`#130`) — move workflows onto the Node 24 compatibility path and standardize the frontend job runtime.
- [x] **P3-03** Add automated dependency update policy (`#131`) — add Dependabot coverage for Go modules, npm, and GitHub Actions.
- [x] **P3-04** Add security scanning for Go and frontend dependencies (`#132`) — wire `govulncheck` and frontend dependency auditing into CI.
- [x] **P3-05** Validate deployment assets in CI (`#135`) — prove `docker compose` and `nginx.conf` stay valid on every change.
- [ ] **P3-06** Add benchmark workflow for repeatable performance regression tracking (`#136`) — separate performance-oriented simulation tests from correctness CI.
