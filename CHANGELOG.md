# Changelog

All notable changes to the Go implementation of Harnas are recorded here.

## [Unreleased]

### Reference implementation (Go)

#### Added

- Added a small `harnas` CLI with `inspect`, `fork`, `diff`, and
  `project` commands for persisted Session JSONL debugging.
- Added a public Agent Manifest loader that validates v0.1 manifests,
  builds the projection/ingestor/registry/strategy bundle, and exposes
  Session-scoped strategy installation.
- Added an Agent façade plus `bin/harnas chat` and `bin/harnas run`
  for manifest-driven buffered turns with automatic Session JSONL
  saving.
- Added built-in tool handlers for read_file, write_file, edit_file,
  list_dir, glob, grep, run_shell, and fetch_url. The Go CLI resolves
  `harnas.builtin.*` manifest handlers automatically.
- Added tool middleware helpers: Timed, Logged, Retried, RateLimiter,
  and Log-sourced StaleReadGuard.
- Added `Permission::AlwaysAllow` and `Permission::HumanApproval`
  strategies, including manifest resolution for HumanApproval prompt
  handlers.
- Added `Compaction::TokenMarkerTail` and `Compaction::SummaryTail`,
  including manifest wiring for provider-backed summaries.
- Projections now include manifest tool descriptors in provider
  requests for Anthropic, OpenAI, and Gemini.
- Added buffered HTTP providers for Anthropic, OpenAI, and Gemini,
  including provider-specific auth headers, Gemini model-in-URL
  request handling, HTTP status errors, and invalid-JSON errors.
- Added streaming HTTP providers for Anthropic, OpenAI, and Gemini,
  with SSE parsing that accepts both LF and CRLF event separators.
- `Agent.Stream` exposes the streaming path and `bin/harnas chat`
  prints assistant text deltas as they arrive.
- Added `RetryPolicy` with retryable HTTP/network decisions and
  configurable backoff, wired into `AgentLoop`.
- Log Events now carry an internal `ID` so Session JSONL save/load can
  preserve Event identity instead of synthesizing ids only while
  writing files.

## [0.4.0] — 2026-04-29

### Reference implementation (Go)

#### Changed

- Conformance now passes 20/20 fixtures, including streaming,
  provider retry/fatal errors, tool failure, permission denial, and
  large/unicode tool arguments.
- Added `Session.Save`, `LoadSession`, and `bin/conformance-roundtrip`
  for Session JSONL cross-language round-trip conformance. The Go
  implementation now participates in the Ruby/Python/Go 3x3
  persistence matrix.
- Added property-style Go tests for mutation idempotence, projection
  purity, dense seq assignment, fork prefixes, and compact/revert
  composition.
- Added `Session.Fork` and conformance fork actions with prefix and
  metadata verification.
- Added mutation application for projections and conformance input
  actions for explicit `compact` / `revert` chains.
- Added the conformance-facing `Compaction::ToolOutputCap` strategy.
- Buffered conformance scripts can now assert the projected provider
  request before returning a response.
- Added OpenAI and Gemini fixture ingestors, session-scoped hooks,
  MarkerTail compaction, DenyByName permission, scripted provider
  errors, and provider_error Log events.
- Scripted streaming fixtures can now model mid-stream provider
  failures by appending `assistant_turn_failed` before raising the
  provider error.

[0.4.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.4.0
