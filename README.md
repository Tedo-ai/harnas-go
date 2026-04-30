# harnas-go

Go implementation of [Harnas](https://github.com/Tedo-ai/harnas), a
specification for LLM agent harnesses.

This repo is a conformance-first peer implementation. It starts with
the smallest buffered AgentLoop surface and grows fixture by fixture
toward parity with the Ruby reference.

**Version 0.4.0** (2026-04-29). Tracks Harnas spec 0.4.0.

## Status

- Agent conformance: 20/20 fixtures passing
- Buffered and streaming AgentLoop paths
- Public Agent Manifest loader for v0.1 manifests
- Agent façade and `bin/harnas chat` / `bin/harnas run`
- Buffered HTTP providers for Anthropic, OpenAI, and Gemini
- Built-in tools: read_file, write_file, edit_file, list_dir, glob,
  grep, run_shell, fetch_url
- Anthropic, OpenAI, and Gemini fixture ingestors
- Session-scoped hooks, MarkerTail and ToolOutputCap compaction,
  AlwaysAllow, DenyByName, and HumanApproval permission
- Scripted provider errors and provider_error Log events

## Run

```sh
go test ./...
bin/conformance
bin/conformance-roundtrip --help
bin/harnas run manifest.json --input "hello"
bin/harnas chat manifest.json
bin/harnas inspect session.jsonl
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
bin/harnas project session.jsonl --manifest manifest.json [--from-seq N] [--to-seq M]
```

`project` renders the provider request body from a saved Log slice
without making a provider call. It supports the conformance-facing
Anthropic, OpenAI, and Gemini projections.
