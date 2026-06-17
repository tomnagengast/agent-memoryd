# Memory Explorer Curation Cockpit

> updated: 2026-06-17

## Goal

Turn `agent-memoryd explore` from a read-only browser into the primary local control surface for memory curation.

## Problem

Memory systems degrade unless users can inspect, edit, delete, and review what has been remembered. The current TUI is a strong start, but it is mostly read/search/view.

Key code:

- `internal/app/app.go:296` defines `explore` with only a `--limit` flag.
- `internal/explore/explore.go:21` exposes only `List`, `Search`, and `Get` on the store interface.
- `internal/explore/explore.go:172` loads records.
- `internal/explore/explore.go:193` searches records.
- `internal/explore/explore.go:99` handles navigation keys, not curation actions.

## Product Thesis

The explorer should be the “memory cockpit”: the place where a user can see what agents know, correct bad memories, remove stale context, and review automatically generated memories.

This is more important than adding more automatic ingestion. Automatic memory without curation becomes untrusted memory.

## Proposed UX

Core list/detail stays the same. Add actions:

```text
/        search
tab      switch filter field
e        edit selected memory
d        delete selected memory
r        mark reviewed
u        mark unreviewed
y        copy id
c        copy body
s        copy source
o        open source if local path
R        reindex
?        help
q/esc    quit
```

Add filter fields:

- query
- project
- kind
- source type, after metadata lands
- reviewed, after metadata lands

## Store Interface Changes

Expand explorer's local interface without binding it to the concrete store implementation:

```go
type Store interface {
    List(context.Context) ([]memory.Record, error)
    Search(context.Context, memory.SearchRequest) ([]memory.SearchResult, error)
    Get(context.Context, string) (memory.Record, error)
    Add(context.Context, memory.AddRequest) (memory.Record, error)
    Forget(context.Context, string) error
    RebuildIndex(context.Context) error
}
```

Editing can initially be simple: open `$EDITOR` with a JSON or markdown-ish representation, then write the updated record through `Add` with the same ID.

## Phased Plan

Phase 1: Safer browse and delete.

- Add help overlay.
- Add project/kind filters.
- Add delete with confirmation.
- Add copy ID/body/source.

Phase 2: Edit and reindex.

- Add editor-based update.
- Add `R` reindex action.
- Surface stale index status once index health exists.

Phase 3: Review workflow.

- Add reviewed/unreviewed filtering after record metadata is added.
- Show daemon-created memories separately from manual memories.
- Add batch delete/review operations.

## Acceptance Criteria

- A user can delete a bad memory from the TUI with confirmation.
- A user can filter by project and kind without leaving the TUI.
- A user can copy a selected memory ID and source.
- A user can run reindex from the TUI and see success/failure.
- Existing search/list/detail layout remains usable on small terminal sizes.

## Tests

- Model update tests for filter input and navigation.
- Delete confirmation flow test.
- Reindex action success/failure test using a fake store.
- Snapshot-ish tests for help/status text where practical.

## Open Questions

- Should editing be JSON, markdown, or field-by-field forms through `huh`?
- Should delete be a hard delete only, or should there be an undo window?
- Should source opening be built in, or should the TUI only copy the source reference?
