# Agent Context

`agent-memoryd` is a local-first memory daemon + MCP server for coding agents: one Go binary exposing the same memory store through CLI commands, an stdio MCP server, and a polling ingest daemon. The user-facing picture is in `README.md` and `docs/` (start with `docs/architecture.md`). This file holds only the non-obvious things a change needs in every session.

## Build / test / verify

`mise` manages tooling (Go pinned in `.mise.toml`; `.envrc` is `use mise`).

- `mise run build` — build `./agent-memoryd` (default **lexical** index, pure Go)
- `mise run test` — `go test ./...`
- `mise run fmt` — `gofmt -w .`
- Single test: `go test ./internal/memory -run TestName`

CI builds the **default (non-zvec) binary**, runs `go test ./...`, and fails if `gofmt -l .` is non-empty. So keep the lexical build green and run `mise run fmt` before pushing.

**Build-tag duality is the thing that bites every build.** The index has two implementations selected by the `zvec` build tag: pure-Go `LexicalIndex` (default, the only thing CI compiles) vs cgo `zvec`. The seam is `internal/indexer.New`. Working on zvec needs native libs first (`mise run zvec-libs`, then `mise run build-zvec`) — and don't break the default path to fix the tagged one.

## Map

Entry is tiny: `cmd/agent-memoryd/main.go` → `internal/app.Run` (cobra tree in `internal/app/app.go`, MCP server in `internal/app/mcp.go`). Four layers, detail in `docs/architecture.md`:

- **Store** (`internal/memory`) — `memories.jsonl` is the source of truth; everything else is derived.
- **Index** (`internal/memory` + `internal/indexer`) — lexical or zvec (see above).
- **Ingest** (`internal/daemon`, `ingest`, `spool`, `ingeststate`) — daemon polls on a ticker; each pass drains the git spool, then scans transcript roots.
- **Retrieval** — CLI and the five MCP tools (`search`/`get`/`add`/`forget`/`reflect`) share one `Store`. MCP is stdio only.

## Invariants worth holding (don't relearn these the hard way)

- **The summarizer is the single funnel for ingest.** Transcript, git, and `reflect` producers never store raw material — they hand it (plus existing memory summaries) to `summarizer.Agent` and store only the distilled body with a `source` / `More detail:` pointer. Don't write raw transcripts, diffs, or git output into a memory body.
- **Git hooks enqueue, they never summarize inline** (`enqueue-git` → spool → daemon picks it up later).
- **`Store` rewrites all of `memories.jsonl` under a mutex** on every add/forget (read → modify → atomic temp+rename). Fine at the expected scale; don't assume streaming or incremental writes.
- **Lifecycle is manifest-driven** (`resources.json`). Add a managed file, hook, or service? Register it in `plannedResources` (`internal/config`) or `status`/`uninstall` silently drift.
- **`internal/config` has paired in-memory and on-disk structs** (durations serialize as strings). Add a field to both or it won't round-trip.

## Workflow

- Commit often, short imperative subjects ("Add OpenCode session ingestion").
- The built `./agent-memoryd`, `.agent-memoryd/`, and `lib/` are gitignored — don't commit them.

# Agent Context
