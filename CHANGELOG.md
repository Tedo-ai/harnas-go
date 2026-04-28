# Changelog

All notable changes to the Go implementation of Harnas are recorded here.

## [Unreleased]

### Reference implementation (Go)

#### Changed

- Conformance now passes 14/14 fixtures, including streaming,
  provider retry/fatal errors, tool failure, permission denial, and
  large/unicode tool arguments.
- Added OpenAI and Gemini fixture ingestors, session-scoped hooks,
  MarkerTail compaction, DenyByName permission, scripted provider
  errors, and provider_error Log events.
- Scripted streaming fixtures can now model mid-stream provider
  failures by appending `assistant_turn_failed` before raising the
  provider error.
