# Changelog

All notable changes to the Go implementation of Harnas are recorded here.

## [Unreleased]

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
