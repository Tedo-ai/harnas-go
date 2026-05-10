# harnas-go

Go implementation of [Harnas](https://github.com/Tedo-ai/harnas), a
specification for LLM agent harnesses.

This repo is a conformance-first peer implementation. It started with
the smallest buffered AgentLoop surface and now includes the live
provider, CLI, tool, middleware, strategy, persistence, and conformance
surfaces needed for real Go adoption.

**Version 0.9.3** (2026-05-10). Tracks Harnas spec 0.9.3.

## Status

- Agent conformance: 28/28 fixtures passing
- Buffered and streaming AgentLoop paths
- Public Agent Manifest loader for v0.1 manifests
- Agent façade and `bin/harnas chat` / `bin/harnas run`
- Buffered HTTP providers for Anthropic, OpenAI, and Gemini
- Streaming HTTP providers for Anthropic, OpenAI, and Gemini
- Built-in tools: read_file, write_file, edit_file, list_dir, glob,
  grep, run_shell, fetch_url, with manifest-ready descriptors
- Tool middleware: Timed, Logged, Retried, RateLimiter, StaleReadGuard
- Anthropic, OpenAI, and Gemini fixture ingestors
- Session-scoped hooks and observation bus, MarkerTail, TokenMarkerTail, SummaryTail,
  and ToolOutputCap compaction,
  AlwaysAllow, DenyByName, and HumanApproval permission
- Scripted provider errors and provider_error Log events
- Observation-only streaming transport events plus DeltaLogger sidecar
  persistence for debugging

## Run

```sh
go test ./...
bin/conformance
bin/conformance-roundtrip --help
bin/harnas run manifest.json --input "hello"
bin/harnas chat manifest.json
bin/harnas inspect session.jsonl
bin/smoke-anthropic "say hello in one word"
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
Anthropic, OpenAI, and Gemini projections.

## Live providers

Set `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, or `GEMINI_API_KEY` to run
the live smoke scripts. Each smoke script exercises both the buffered
and streaming provider for that backend:

```sh
bin/smoke-anthropic "say hello in one word"
bin/smoke-openai "say hello in one word"
bin/smoke-gemini "say hello in one word"
```
