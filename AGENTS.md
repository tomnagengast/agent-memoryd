# Agent Context

`agent-memoryd` is a local-first memory daemon + MCP server for coding agents: one Go binary exposing the same memory store through CLI commands, an stdio MCP server, and a polling ingest daemon. The user-facing picture is in `README.md` and `docs/` (start with `docs/architecture.md`). This file holds only the non-obvious things a change needs in every session.

## Build / test / verify

`mise` manages tooling (Go pinned in `.mise.toml`; `.envrc` is `use mise`).

- `mise run zvec-libs` - download native zvec libraries into `./lib/`
- `mise run build` - build `./memoryd` with cgo + zvec
- `mise run test` - run Go tests, using zvec when local native libs are present
- `mise run fmt` - `gofmt -w .`
- Single test: `go test ./internal/memory -run TestName`

CI downloads zvec native libraries, runs `go test ./...` with cgo enabled, builds `./memoryd`, and fails if `gofmt -l .` is non-empty. Keep the zvec-backed build green.

## Map

Entry is tiny: `cmd/agent-memoryd/main.go` → `internal/app.Run` (cobra tree in `internal/app/app.go`, MCP server in `internal/app/mcp.go`). Four layers, detail in `docs/architecture.md`:

- **Store** (`internal/memory`) - zvec at `$MEMORYD_HOME/zvec` is the source of truth. Legacy `memories.jsonl` files are imported once and renamed with a `.migrated` suffix.
- **Retrieval** (`internal/memory`) - hybrid full-text + vector search, with nullable embeddings and `reindex` for backfill.
- **Ingest** (`internal/daemon`, `ingest`, `spool`, `ingeststate`) - daemon polls on a ticker; each pass drains the git spool, then scans transcript roots.
- **IPC / MCP** (`internal/storerpc`, `internal/app/mcp.go`) - the daemon owns the zvec store and CLI/MCP operations route through the Unix socket. MCP is stdio only.

## Invariants worth holding (don't relearn these the hard way)

- **The summarizer is the single funnel for ingest.** Transcript, git, and `reflect` producers never store raw material. They hand it (plus existing memory summaries) to `summarizer.Agent` and store only the distilled body with a `source` / `More detail:` pointer. Don't write raw transcripts, diffs, or git output into a memory body.
- **Git hooks enqueue, they never summarize inline** (`enqueue-git` → spool → daemon picks it up later).
- **The daemon is the store owner.** zvec takes an exclusive lock, so CLI and MCP store operations should route through the daemon socket rather than opening the collection directly.
- **Embedding is best-effort.** Records without vectors remain durable and full-text searchable; `reindex` backfills embeddings later.
- **Lifecycle is manifest-driven** (`resources.json`). Add a managed file, hook, or service? Register it in `plannedResources` (`internal/config`) or `status`/`uninstall` silently drift.
- **`internal/config` has paired in-memory and on-disk structs** (durations serialize as strings). Add a field to both or it won't round-trip.

## Workflow

- Commit often, short imperative subjects ("Add OpenCode session ingestion").
- The built `./memoryd`, `.agent-memoryd/`, and `lib/` are gitignored. Don't commit them.

# Agent Context
