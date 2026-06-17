# Performance And Reliability Test Coverage

> updated: 2026-06-17

## Goal

Add targeted tests and lightweight benchmarks around the areas most likely to fail as `agent-memoryd` grows: concurrent store writes, large stores, daemon ingestion, spool robustness, summarizer behavior, and zvec parity.

## Current Health

Verified locally:

```sh
go test ./...
go test -race ./...
```

Both pass.

`mise run test` is currently blocked in this shell because `.mise.toml` is not trusted, but the underlying command is `go test ./...` per `.mise.toml`.

## Current Coverage Shape

Good package-level tests exist for:

- `internal/app`
- `internal/config`
- `internal/explore`
- `internal/githooks`
- `internal/importmem`
- `internal/ingest`
- `internal/ingeststate`
- `internal/memory`
- `internal/spool`
- `internal/summarizer`

Gaps are around cross-process behavior, failure recovery, large inputs, zvec backend behavior, and daemon integration.

## Priority Test Areas

## Store

Target files:

- `internal/memory/store.go`
- `internal/memory/record.go`
- `internal/memory/search.go`

Add tests for:

- concurrent cross-process writes after file locking lands.
- index update failure after successful source write.
- stale index status and recovery via reindex.
- duplicate IDs in imported/manual JSONL.
- update semantics for omitted fields.
- Unicode-safe summaries.
- records larger than scanner defaults.
- large-store search/list timing benchmarks.

Potential benchmarks:

```go
BenchmarkStoreAdd1K
BenchmarkStoreAdd10K
BenchmarkStoreSearchLexical10K
BenchmarkStoreList10K
```

## Daemon And Transcript Ingest

Target files:

- `internal/daemon/daemon.go`
- `internal/ingest/transcript.go`
- `internal/ingeststate/state.go`

Add tests for:

- idle checks using injectable time rather than wall clock.
- huge transcript lines.
- transcript modification after prior ingest.
- empty/non-conversation JSONL not being repeatedly rescanned.
- newest transcript selection avoiding active sessions if that behavior is added.
- source material caps/truncation when implemented.

## Git Spool

Target file:

- `internal/spool/spool.go`

Add tests for:

- atomic enqueue temp-file-plus-rename once implemented.
- malformed JSON quarantine.
- partial event files.
- failed `git show` behavior.
- failed-dir filename collisions.
- large backlog handling.

## Summarizer

Target file:

- `internal/summarizer/summarizer.go`

Add tests for:

- timeout behavior.
- large stdout/stderr handling.
- fenced JSON with commentary.
- malformed JSON recovery/error messages.
- prompt size limiting once implemented.

## zvec Backend

Target files:

- `internal/zvecindex/index_zvec.go`
- `internal/indexer/indexer_zvec.go`

Add `-tags zvec` tests for:

- empty query behavior parity with lexical.
- project/kind filters.
- rebuild deleting stale docs.
- delete behavior.
- search without source-store read after interface split.
- parity expectations for simple lexical-like fixtures.

These may run only on supported platforms or in a separate CI job once native libs are available.

## CI Improvements

Current CI runs format, tests, and build: `.github/workflows/ci.yml`.

Recommended additions:

- `go test -race ./...` on PRs if runtime remains acceptable.
- Benchmark job manually triggered or nightly, not required on every PR.
- Optional zvec tagged test job once native library setup is reliable.
- `go vet ./...` if it stays low-noise.

## Acceptance Criteria

- Store write safety changes are covered by tests that fail on the current implementation.
- Spool malformed/partial event handling is covered.
- Summarizer timeout and bad-output paths are covered.
- Race detector remains green.
- Benchmarks exist for store/search scaling, even if not enforced in CI.

## Open Questions

- Should large-store benchmarks use generated temp files or in-memory fixtures?
- Should zvec tests be required in CI or remain a local/manual gate?
- What store-size target should define acceptable MVP performance: 1k, 10k, or 100k memories?
