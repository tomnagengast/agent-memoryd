# Store Write Safety And Index Health

> updated: 2026-06-17

## Goal

Make `memories.jsonl` safe as the source of truth across CLI, MCP, daemon, import, and future long-running processes. Preserve the current local-first JSONL design while eliminating lost updates, temp-file collisions, and silent source/index drift.

## Problem

The current store is intentionally simple, but its safety boundary is only process-local. `Store` uses a `sync.Mutex`, then reads all records, rewrites the full file, and updates the derived index after the source write.

Key code:

- `internal/memory/store.go:54` locks only the current process.
- `internal/memory/store.go:61` reads the full source store for `Add`.
- `internal/memory/store.go:80` writes the source file before index update.
- `internal/memory/store.go:83` updates the derived index after the source has changed.
- `internal/memory/store.go:233` uses a fixed temp path, `memories.jsonl.tmp`.
- `internal/memory/store.go:248` renames the temp file into place without file or directory fsync.

Risks:

- Concurrent CLI/MCP/daemon writes can lose data.
- Multiple writers can collide on the same temp file.
- A successful source write followed by failed index update leaves stale derived data.
- Power loss can lose recently acknowledged writes.

## Proposed Shape

Keep `memories.jsonl` as the durable source of truth. Add a small write-safety layer around the existing store implementation.

Core pieces:

- Cross-process file lock scoped to the store path.
- Unique temp files for each write.
- Atomic rename after successful encode and close.
- Best-effort fsync of temp file and parent directory.
- Index health marker when index mutation fails after source mutation.
- `status` reporting for index health and stale-index recovery instructions.

## Design Notes

Use a lock file beside the store, for example:

```text
$AGENT_MEMORYD_HOME/memories.jsonl.lock
```

On Unix-like systems, use `flock`/`fcntl` through `golang.org/x/sys/unix`. If Windows support becomes a near-term target, isolate the lock implementation behind a small internal package.

Use temp files with unique names:

```text
memories.jsonl.<pid>.<random>.tmp
```

Index health should be persisted separately from the index itself, because lexical index has no durable artifact. A small status file is enough:

```json
{
  "stale": true,
  "reason": "update memory index: ...",
  "record_id": "...",
  "updated_at": "2026-06-17T00:00:00Z"
}
```

Potential path:

```text
$AGENT_MEMORYD_HOME/index-status.json
```

## Implementation Plan

1. Add an internal locking helper.
2. Change store writes to acquire the cross-process lock before read-modify-write.
3. Replace fixed temp path with unique temp path in the same directory.
4. Add file and parent directory fsync where supported.
5. Add index health persistence and clear it after successful `reindex`.
6. Expose index health in `Status` and `status` command output.
7. Add tests for concurrent writes and index failure recovery.

## Batch API Follow-Up

Add batch operations after write safety lands:

```go
func (s *Store) AddBatch(ctx context.Context, reqs []memory.AddRequest) ([]memory.Record, error)
func (s *Store) ForgetBatch(ctx context.Context, ids []string) error
```

This avoids repeated full-file reads and rewrites in import, transcript ingest, git ingest, and reflect paths.

Initial call sites:

- `internal/importmem/importmem.go`
- `internal/ingest/transcript.go`
- `internal/spool/spool.go`
- `internal/app/mcp.go` reflect path

## Acceptance Criteria

- Two concurrent processes adding different records do not lose either record.
- Store write temp files do not collide across processes.
- If index update fails after source write, `status` reports a stale index.
- `agent-memoryd reindex` clears stale index status after successful rebuild.
- Existing CLI/MCP behavior remains compatible.

## Tests

- Concurrent multi-process add test using a shared temp data root.
- Index adapter test double that fails on `Upsert` after source write.
- Reindex clears stale status.
- Store writes clean up temp files on encode failure.
- Unicode and large-record cases still behave correctly.

## Open Questions

- Should stale index status block zvec search, warn only, or fall back to lexical?
- Should lock acquisition have a timeout?
- Should `status` include lock owner/debug metadata when locked?
