# harnas-go

Go implementation of [Harnas](https://github.com/Tedo-ai/harnas), a
specification for LLM agent harnesses.

This repo is a conformance-first peer implementation. It started with
the smallest buffered AgentLoop surface and now includes the live
provider, CLI, tool, middleware, strategy, persistence, and conformance
surfaces needed for real Go adoption.

**Version 0.17.0** (2026-05-21). Tracks Harnas spec 0.17.0.

## Status

- Agent conformance: 54/54 fixtures passing
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
