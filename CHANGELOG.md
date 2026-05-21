# Changelog

All notable changes to the Go implementation of Harnas are recorded here.

## [0.17.0] — 2026-05-21

### Added

- Added multimodal content block support for text, image, and PDF
  document message content.
- Added AttachmentStore helpers: filesystem, memory, and inline stores.
- Updated Anthropic, OpenAI, Gemini, and Ollama projections for
  multimodal content and provider capability mismatch fallback.
- Added CLI `--input-file` support for image and PDF attachments.
- Updated `TranscriptProject` to render non-text content placeholders.

### Changed

- Lockstep spec release. Validated against fixtures version `0.17.0`.
- Conformance now passes 54/54 fixtures, including the eight
  multimodal content fixtures.

## [0.16.0] — 2026-05-21

### Added

- Added `credential/proxy`, a `:pre_tool_use` strategy that injects
  credential-backed headers into supported tool arguments while keeping
  credential values out of the Log and Observation stream.
- `fetch_url` now accepts optional request headers so credential/proxy can
  authorize HTTP calls without exposing secrets to the model.

### Changed

- Lockstep spec release. Validated against fixtures version `0.16.0`.
- Conformance now passes 46/46 fixtures, including
  `with-credential-proxy-injection`.

## [0.15.0] — 2026-05-21

### Added

- Added `mcp/`, a Model Context Protocol adapter package with HTTP
  POST and stdio transports, MCP content flattening, Harnas tool
  descriptor translation, dynamic passthrough tool handlers, custom
  HTTP headers, lazy initialization, and degraded startup handling.
- Tool handlers may now use the explicit `ToolHandlerV2` /
  `ConfiguredToolHandler` shape to receive the tool manifest config map.
  Existing single-argument `ToolHandler` callables continue to work
  unchanged; `WrapV1Handler` adapts them when a V2 handler is required.

### Changed

- First non-lockstep release. `harnas-ruby` remains at v0.14.1 with no
  functional change, and the spec remains at v0.14.1 with no spec
  change. The lockstep discipline applies to spec changes; library
  feature additions may now ship independently per implementation.

## [0.14.1] — 2026-05-21

### Added

- Conformance runner now supports `--fixtures-from` and reports the
  fixtures version from the spec repo `VERSION` file.
- Added `harnas conformance` so packed binaries can run conformance
  without the source-tree `bin/conformance` wrapper.
- Added packed-binary conformance CI: build `cmd/harnas`, then run
  conformance through the built binary.

### Changed

- Validated against fixtures version `0.14.1`.

## [0.14.0] — 2026-05-21

### Added

- Added `sandbox/network`, a tool-boundary network strategy with exact host
  allow/deny enforcement for `fetch_url`.
- Extended `harnas.builtin.bash_session` so `run` accepts an optional
  per-command `env` object whose variables do not persist in the shell
  session.

### Changed

- Updated `harnas.builtin.read_file` to accept `offset` and `limit`, return
  `cat -n` style line-numbered output, and reject binary files.
- Conformance now passes 45/45 fixtures.

## [0.13.0] — 2026-05-18

### Added

- Added `guard/health`, a pre-provider health-check strategy.
- Extended `guard/repetition` to detect repeated approval rejections.
- Added Ollama buffered and streaming providers using Ollama's
  OpenAI-compatible `/v1/chat/completions` endpoint, plus
  `bin/smoke-ollama`.

## [0.12.0] — 2026-05-18

### Added

- Added `sandbox/write`, `guard/repetition`, `guard/timeout`, and
  `guard/cost_budget` strategies.
- Added `--output-format ndjson` for `bin/harnas run`.
- Applied the shared CLI exit-code taxonomy and partial stdout flush on
  exit-1 agent failures.
- Conformance now passes 39/39 fixtures.

## [0.11.0] — 2026-05-17

### Added

- Promoted `harnas.builtin.bash_session` to the conformable surface, a
  persistent shell-session built-in for sandboxed coding agents. It preserves
  working directory and environment changes across calls to the same
  session, supports `run` / `status` / `kill`, strips ANSI output,
  tail-truncates large stdout/stderr buffers, and returns canonical JSON
  as `tool_result.output`.
- Added `examples/bash-session/`, a minimal live-provider manifest for
  trying `bash_session` from the CLI.
- Added `command_stdout` and `command_stderr` to the `bash_session`
  result, so agents can reason over the current command
  without subtracting earlier session output from the cumulative
  transcript.
- Added adopter helper surfaces: `NewRuntime`, `TranscriptProject`,
  `ToolDescriptors`, and `ManifestSnapshotMetadata`.
- Conformance now passes 34/34 fixtures, including the four
  `bash_session` fixtures.

## [0.10.0] — 2026-05-10

### Added

- Added `BuildSkillsIndex`, which scans a skills directory and emits
  the canonical `## Skills` system-prompt section.
- Added the `harnas.builtin.load_skill` built-in tool with
  config-driven `skills_dir`, frontmatter stripping, skill-name
  validation, and empty-body support.
- Conformance now passes 30/30 fixtures, including `with-skills` and
  `with-skills-invalid-name`.

## [0.9.3] — 2026-05-10

### Informative

- Tracks the v0.9.3 spec, which adds non-normative ecosystem
  conventions for skills and MCP mappings. No Go runtime behavior
  changes; the `load_skill` built-in and skills-index helper are
  planned for v0.10.

## [0.9.2] — 2026-05-08

### Fixed

- `OpenAIProjection.Project` now folds `:tool_use` events into the
  preceding assistant message's `tool_calls[]` and emits `:tool_result`
  as a `role: "tool"` message with `tool_call_id`. Earlier 0.9.x
  releases silently dropped these events, breaking multi-turn
  tool-calling against OpenAI-protocol providers (OpenAI, OpenRouter,
  Together, Grok, Kimi, etc.). Ruby and Python ports already had this;
  Go was the outlier.
- `OpenAIProjection.Project` now sets `content: null` on assistant
  messages that carry `tool_calls[]`, matching the Ruby/Python ports
  and OpenAI's documented convention.

### Conformance

- The shared `with-tool-call-openai` fixture now asserts on the second
  projected request (via `expect_request`), proving prior `tool_calls`
  and the `role: tool` reply survive the round-trip. This is the gap
  that let the projection bug pass earlier CI.

## [0.9.1] — 2026-05-05

### Trust polish

- Updated README version, fixture-count, and Go install language to
  match the verified v0.9.1 surface.
- Added normal push/PR CI for gofmt, `go test`, and conformance.

### v0.9.1

#### Added

- Manifest tool entries may now declare opaque `config`; the Go loader
  stores it in the Session manifest snapshot and exposes it on `Tool`
  records, with optional configured handler support.
- Conformance now passes 28/28 fixtures, including
  `with-tool-config-roundtrip`.

## [0.9.0] — 2026-05-05

### Added

- Added manifest-declared hook installation, `on_error: "fail_turn"`
  hook policy support, and terminal `runtime_error` Log events for
  harness-internal failures.
- Added `CostTracker` for cumulative token usage tracking.
- Strategies now emit Observation-only `strategy_started` and
  `strategy_completed` events with `noop`, `mutated`, `refused`, or
  `error` effects.
- Conformance now passes 27/27 fixtures, including manifest hooks,
  fail-turn runtime errors, and strategy-event sidecars.

### Fixed

- `StaleReadGuard` now allows creation of a file that does not yet exist
  on disk while still refusing overwrites of existing files that have
  not been read in the current Session.
- Clarified `StaleReadGuard` refusal messages so LLM consumers know when
  to call `read_file` before retrying a write/edit.

## [0.8.0] — 2026-05-03

### Reference implementation (Go)

#### Changed

- Streaming transport events now emit on the Session Observation bus
  and no longer append to the durable Log. Consolidated
  `assistant_message` / `tool_use` events still append as before.
- `Agent.Stream` and `bin/harnas chat` now stream from the AgentLoop
  callback rather than re-appending provider events.
- Conformance now passes 24/24 fixtures, including the
  `with-delta-logger-sidecar` fixture.

#### Added

- Added `DeltaLogger` for opt-in sidecar JSONL persistence of
  streaming transport events.

#### Fixed

- OpenAI live streaming requests include
  `stream_options: {"include_usage": true}`, preserving non-zero usage
  in the consolidated assistant message.

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

[0.17.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.17.0
[0.16.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.16.0
[0.15.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.15.0
[0.14.1]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.14.1
[0.14.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.14.0
[0.13.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.13.0
[0.12.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.12.0
[0.11.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.11.0
[0.10.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.10.0
[0.9.3]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.9.3
[0.9.2]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.9.2
[0.9.1]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.9.1
[0.9.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.9.0
[0.8.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.8.0
[0.7.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.7.0
[0.6.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.6.0
[0.5.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.5.0
[0.4.0]: https://github.com/Tedo-ai/harnas-go/releases/tag/v0.4.0
