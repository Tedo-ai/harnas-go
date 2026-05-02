# Changelog

All notable changes to the Go implementation of Harnas are recorded here.

## [Unreleased]

## [0.7.0] — 2026-05-02

### Reference implementation (Go)

#### Added

- `assistant_message` payloads now preserve optional reasoning block
  lists.
- Anthropic, OpenAI, and Gemini ingestors capture provider reasoning
  content into `payload.reasoning` when present.
- The Anthropic projection round-trips captured reasoning as thinking
  content blocks, including signatures, for follow-up turns.
- Conformance now passes 23/23 fixtures, including reasoning capture
  for Anthropic, OpenAI, and Kimi-shaped OpenAI-compatible responses.

## [0.6.0] — 2026-05-02

### Reference implementation (Go)

#### Changed

- No Go code changes. This release keeps the Go implementation aligned
  with the spec and sibling implementation tags while harnas-python
  reaches the same public feature surface.

## [0.5.0] — 2026-05-02

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
- Added live provider smoke scripts for Anthropic, OpenAI, and Gemini.
  Each script exercises both buffered and streaming providers, and a
  scheduled GitHub Actions workflow runs them weekly.
- Added `RetryPolicy` with retryable HTTP/network decisions and
  configurable backoff, wired into `AgentLoop`.
- Log Events now carry an internal `ID` so Session JSONL save/load can
  preserve Event identity instead of synthesizing ids only while
  writing files.
- `bin/harnas` now matches the Ruby CLI's provider/model override
  defaults, provider-error formatting, and projection range
  validation.
- The manifest loader now rejects unknown fields, missing required
  manifest keys, empty `system`, and malformed tool/strategy shapes,
  with typed Go errors for validation, version, provider, strategy,
  and handler failures.
- Added a Session-scoped `Observation` bus plus core lifecycle
  emissions for Log append, projection, provider calls, provider
  responses/failures, and hook handler failures.
- Added `BuiltinDescriptors()` so Go exposes the same manifest-ready
  built-in tool descriptors as Ruby.

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
