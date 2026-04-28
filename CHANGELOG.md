# Changelog

All notable changes to the Go implementation of Harnas are recorded here.

## [Unreleased]

### Reference implementation (Go)

#### Changed

- Conformance now passes 20/20 fixtures, including streaming,
  provider retry/fatal errors, tool failure, permission denial, and
  large/unicode tool arguments.
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
