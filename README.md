# harnas-go

Go implementation of [Harnas](https://github.com/Tedo-ai/harnas), a
specification for LLM agent harnesses.

This repo is a conformance-first peer implementation. It started with
the smallest buffered AgentLoop surface and now includes the live
provider, CLI, tool, middleware, strategy, persistence, and conformance
surfaces needed for real Go adoption.

**Version 0.20.1** (2026-06-27). Tracks Harnas spec 0.22.0.

## Status

- Agent conformance: 78/78 fixtures passing
- Raw provider-wire conformance: 18/18 cases and 39/39 deterministic
  byte-fragmented executions through the production Anthropic, OpenAI, and
  Gemini stream parsers
- Buffered and streaming AgentLoop paths
- Public Agent Manifest loader for v0.1 manifests
- Agent façade and `bin/harnas chat` / `bin/harnas run`
- Buffered HTTP providers for Anthropic, OpenAI, Gemini, and local Ollama
- Streaming HTTP providers for Anthropic, OpenAI, Gemini, and local Ollama
- Built-in tools: read_file, write_file, edit_file, list_dir, glob,
  grep, run_shell, fetch_url, load_skill, bash_session, with
  manifest-ready descriptors
- Tool middleware: Timed, Logged, Retried, RateLimiter, StaleReadGuard
- Anthropic, OpenAI, and Gemini fixture ingestors
- Session-scoped hooks and observation bus, MarkerTail, TokenMarkerTail, SummaryTail,
  and ToolOutputCap compaction, AlwaysAllow, DenyByName, HumanApproval,
  sandbox/write, sandbox/network, credential/proxy, repetition,
  health, timeout, and cost-budget guards
- Scripted provider errors and provider_error Log events
- Observation-only streaming transport events plus DeltaLogger sidecar
  persistence for debugging
- Adopter helper APIs: `NewRuntime`, `TranscriptProject`,
  `ToolDescriptors`, `ManifestSnapshotMetadata`, and delegation
  projections (`DelegationTree`, `DescendantTimeline`, `OpenChildren`,
  `DescendantUsage`)
- MCP adapter package: HTTP and stdio transports, content flattening,
  Harnas tool descriptor translation, and degraded startup handling
- Subagent delegation events, cross-session projection helpers, and
  optional `spawn_agent` receipt built-in
- v0.20 durability APIs: `harnas-jcs-v1`
  canonicalization, Event row `content_hash`, memory/file/SQL-backed
  `StorageAdapter` implementations, and `expected_next_seq` OCC fences

## Run

```sh
go test ./...
bin/conformance
bin/conformance-roundtrip --help
bin/harnas run manifest.json --input "hello"
bin/harnas chat manifest.json
bin/harnas inspect session.jsonl
bin/smoke-anthropic "say hello in one word"
bin/smoke-ollama "say hello in one word"
```

Library use:

```sh
go get github.com/Tedo-ai/harnas-go
```

CLI use from source:

```sh
go install github.com/Tedo-ai/harnas-go/cmd/harnas@latest
```

`bin/conformance` resolves fixtures from a sibling checkout of
`Tedo-ai/harnas`, or from `HARNAS_SPEC` when set.

## Write-through persistence

By default the Log is in-memory and hosts persist snapshots with
`Session.Save(path)`. For per-event durability, bind a `StorageAdapter` to the
Session: every `Log.Append` then becomes durable in the adapter — under the
OCC fence (`expected_next_seq`) — *before* the event is visible in memory or
to the next loop step.

```go
db, _ := sql.Open("postgres", dsn)
_ = harnas.EnsureSQLStorageSchema(db, opts)

session := harnas.CreateSession(metadata)
adapter := harnas.NewSQLStorageAdapter(db, session.ID, opts)
if err := session.BindStorage(adapter); err != nil { /* ... */ }

// ... run turns; every event is durable before the next loop step ...

restored, err := harnas.LoadSessionFromStorage(adapter) // resume later
```

Semantics:

- A write-through failure (storage outage or `StorageConflictError` from a
  concurrent writer) is a first-class loop signal: `AgentLoop.Run` returns a
  typed `*StorageWriteError`, the failed event is neither persisted nor in
  memory, and the Log latches against further appends. Recover by reloading
  from the adapter (`LoadSessionFromStorage`) or, after the outage is
  resolved, `Log.ClearStorageErr()` and retry the turn.
- Tool-affecting events are durable before the tool runs: the `tool_use` is
  written through before `dispatchPendingTools` executes it. A crash after a
  tool executed but before its `tool_result` append leaves that `tool_use`
  pending in the durable transcript — see the resume note below.
- Compaction (`compact`/`revert`/`summary`) only appends marker events; it
  never rewrites or renumbers durable rows.
- Events appended through (or restored from) an adapter carry the stored
  harnas-jcs-v1 row hash in `Event.ContentHash` (in-memory only, not part of
  the Session JSONL wire shape).
- Sessions without a binding keep the in-memory behavior verbatim; forks do
  not inherit the binding.

### Resume semantics with real providers

`AgentLoop.Run` calls the provider at the **top** of each turn, before pending
tools are dispatched. A session reloaded from a durable transcript whose last
assistant turn ends in an *un-fulfilled* `tool_use` therefore projects a
request that ends in an assistant tool_use block with no following
tool_result — which live providers (Anthropic/OpenAI) reject, even though a
scripted or mock provider will happily continue. If you implement pause/resume
(approval inboxes, crash recovery mid-tool), fulfill the pending `tool_use`
first — execute it (idempotently) and append its `tool_result`, or append a
synthesized rejection — *before* re-entering `Run()`.

## Operator CLI

The Go port ships the persisted-Session operator commands shared with
the Ruby and Python CLIs:

```sh
bin/harnas run manifest.json --input "hello"
bin/harnas chat manifest.json
bin/harnas inspect session.jsonl [--json]
bin/harnas fork session.jsonl --at-seq N --out forked.jsonl
bin/harnas diff a.jsonl b.jsonl
bin/harnas project session.jsonl --manifest manifest.json [--from-seq N] [--to-seq M] [--provider KIND] [--model MODEL]
```

`project` renders the provider request body from a saved Log slice
without making a provider call. It supports the conformance-facing
Anthropic, OpenAI-compatible, and Gemini projections.

## MCP

The Go port includes `github.com/Tedo-ai/harnas-go/mcp` for consuming
Model Context Protocol servers as Harnas tools. Connect to an MCP
server, ask it for translated tool descriptors, and pass its dynamic
handlers to the runtime:

```go
import "github.com/Tedo-ai/harnas-go/mcp"

mcpClient, err := mcp.Connect(mcp.ConnectOptions{
    URL:        "http://localhost:3001",
    ServerName: "editorial-ai",
    Headers:    map[string]string{"Authorization": "Bearer " + token},
})
if err != nil {
    return err
}
defer mcpClient.Close()

tools, err := mcpClient.Tools(ctx)
if err != nil {
    return err
}
handlers := mcpClient.ToolHandlers()

manifest.Tools = append(manifest.Tools, tools...)
loaded, err := harnas.BuildManifest(manifest, harnas.ManifestOptions{
    ConfiguredHandlers: handlers,
})
if err != nil {
    return err
}
runtime := &harnas.Runtime{Loaded: loaded}
```

`Tools(ctx)` performs lazy MCP initialize + `tools/list`, caches the
translated descriptors, and degrades to an empty tool list if the MCP
server is unavailable. `ToolHandlers()` returns the
`mcp_passthrough.<server>` handler required by those descriptors.

## Live providers

Set `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, or `GEMINI_API_KEY` to run
the remote live smoke scripts. Ollama uses `OLLAMA_BASE_URL` when set
and otherwise defaults to `http://localhost:11434/v1`; its smoke skips
cleanly when Ollama is not running. Each smoke script exercises both the
buffered and streaming provider for that backend:

```sh
bin/smoke-anthropic "say hello in one word"
bin/smoke-openai "say hello in one word"
bin/smoke-gemini "say hello in one word"
bin/smoke-ollama "say hello in one word"
```

## bash_session

The Go port includes the conformable `harnas.builtin.bash_session`
handler. It runs a long-lived shell per named session, preserving `cd`
and `export` across tool calls, and returns a JSON object encoded as the string
`tool_result.output`. The result includes both cumulative `stdout` /
`stderr` and command-local `command_stdout` / `command_stderr`.

Prefer this tool for sandboxed coding agents that can safely expose a
shell. The narrower `list_dir`, `glob`, `grep`, and `run_shell` tools
remain available and are still the safer fit for restricted agents.

A minimal live-provider manifest is available at
`examples/bash-session/manifest.json`:

```sh
export OPENAI_API_KEY=...
bin/harnas chat examples/bash-session/manifest.json
```
