# harnas-go

Go implementation of [Harnas](https://github.com/Tedo-ai/harnas), a
specification for LLM agent harnesses.

This repo is a conformance-first peer implementation. It starts with
the smallest buffered AgentLoop surface and grows fixture by fixture
toward parity with the Ruby reference.

## Status

- `minimal-chat`: passing
- Remaining fixtures: in progress

## Run

```sh
go test ./...
bin/conformance --fixture minimal-chat
```

`bin/conformance` resolves fixtures from a sibling checkout of
`Tedo-ai/harnas`, or from `HARNAS_SPEC` when set.
