# harnas-go

Go implementation of [Harnas](https://github.com/Tedo-ai/harnas), a
specification for LLM agent harnesses.

This repo is a conformance-first peer implementation. It starts with
the smallest buffered AgentLoop surface and grows fixture by fixture
toward parity with the Ruby reference.

## Status

- Agent conformance: 19/19 fixtures passing
- Buffered and streaming AgentLoop paths
- Anthropic, OpenAI, and Gemini fixture ingestors
- Session-scoped hooks, MarkerTail and ToolOutputCap compaction,
  DenyByName permission
- Scripted provider errors and provider_error Log events

## Run

```sh
go test ./...
bin/conformance
```

`bin/conformance` resolves fixtures from a sibling checkout of
`Tedo-ai/harnas`, or from `HARNAS_SPEC` when set.
