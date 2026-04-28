# Changelog

All notable changes to the Go implementation of Harnas are recorded here.

## [Unreleased]

### Reference implementation (Go)

#### Changed

- Conformance now passes 13/13 fixtures, including streaming,
  provider retry/fatal errors, tool failure, permission denial, and
  large/unicode tool arguments.
- Added OpenAI and Gemini fixture ingestors, session-scoped hooks,
  MarkerTail compaction, DenyByName permission, scripted provider
  errors, and provider_error Log events.
