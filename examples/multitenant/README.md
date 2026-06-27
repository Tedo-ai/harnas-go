# Embedding harnas-go in a concurrent, multi-tenant server

A reference for the shape Tedo/Ovin uses: one Session per conversation persisted
to Postgres via the SQL adapter, tools routed through the host's operations
registry behind an enforce-by-default `pre_tool_use` gate, and the Observation
bus bridged to tracing. `embed.go` compiles against harnas-go; the runnable test
(`embed_test.go`) uses `MemoryStorageAdapter` + `MockProvider` so it runs with no
DB or keys. Swap those two for the SQL adapter + a live provider in production —
every other seam is identical.

## The five seams, and how a real stack maps onto them

| Seam (in `embed.go`) | Generic | Ovin mapping |
|---|---|---|
| `loadSession` / `persistNewEvents` | rebuild Log from `EventsSince`, append new events with the OCC fence | one Session per conversation/agent-run, resumed; `workspace_id` from `WorkspaceContextMiddleware` (not client input) |
| `claimTurn` | fail-fast `pg_try_advisory_lock` before `loop.Run()` | the cross-instance turn fence — your in-process mutex generalized; one in-flight turn per session |
| `buildRegistry` | tool `Call` closures funnel through one invoke path | `operations.Invoke(ctx, mw.Call{OperationKey: op.Key, ...})`; tools = the `AIEnabled` subset of `OperationDefinition`, keyed by `op.Key` |
| `installEnforceGate` | `pre_tool_use` → `{allow,reason}` / nil | your actor-permission check; `notes.create_note` (a Command) is gated, the list query allowed |
| `bridgeObservationToTracing` | `provider_called`→span, correlate tools by `tool_use_id` | `internal/tracing.Client` over imprint-go; seed `metadata.trace.*` from inbound `traceparent` |

## Two things to know

1. **The OCC fence is post-execution.** `persistNewEvents` runs *after* `loop.Run()`,
   so the storage-layer fence (`AppendEvent` + `expectedNextSeq`) catches a lost
   race only after side effects ran. For cross-instance safety, take `claimTurn`
   *before* `loop.Run()`. This is intentionally a host concern (orchestration
   policy is out of scope for the spec). A portable pre-execution claim is a
   queued spec ask.

2. **Workspace keying is composed today.** Until the SQL adapter gains a native
   `workspace_id` column, scope by composing the key (`workspace_id + ":" +
   sessionID`), as `newProductionAdapter` shows. When the column lands, switch to
   the native param + `(workspace_id, session_id)` index and backfill.

## Production swaps

- `MemoryStorageAdapter` → `newProductionAdapter` (SQL, Postgres, SQLSTATE-23505
  `ConflictDetector` for exact unique-violation detection — driver-free).
- `MockProvider` → `AnthropicProvider` / `AnthropicStreamProvider`.
- Run `VerifySessionPortable(session)` in CI on a representative session as the
  acceptance gate against hand-rolled persistence that bypasses the adapter.
