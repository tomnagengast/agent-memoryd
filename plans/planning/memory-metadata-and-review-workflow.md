# Memory Metadata And Review Workflow

> updated: 2026-06-17

## Goal

Add minimal metadata that supports trust, curation, and memory quality without turning the record model into a heavy database schema.

## Problem

The current record model is intentionally simple:

```go
type Record struct {
    ID        string
    Kind      string
    Project   string
    Source    string
    Summary   string
    Body      string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

Defined at `internal/memory/record.go:10`.

This is a good MVP, but it cannot answer important curation questions:

- Was this manually added or daemon-generated?
- Has a human reviewed it?
- Which ingestion path created it?
- Which memories are stale or low quality?
- Which automatic memories should be checked before broad retrieval?

## Proposed Minimal Metadata

Add two fields first:

```go
SourceType string `json:"source_type,omitempty"`
Reviewed   bool   `json:"reviewed,omitempty"`
```

Recommended source types:

- `manual`
- `mcp-add`
- `reflect`
- `transcript`
- `git`
- `import`

Do not add tags, confidence, decay, or last-used timestamps in the first pass. Those may become useful later, but source type and reviewed state unlock the immediate product need: trust and curation.

## Compatibility

Existing JSONL records without these fields remain valid.

Defaults:

- Direct CLI `add`: `manual`
- MCP `add`: `mcp-add`
- `reflect`: `reflect`
- transcript daemon: `transcript`
- git daemon: `git`
- import: `import`
- existing records loaded from disk: empty source type, shown as `unknown` in UI/status only

`Reviewed` should default to false for generated/imported records. Manual adds can default to true or false. Recommendation: manual CLI adds default true, MCP adds default false unless clients set it explicitly later.

## API Shape

Extend `memory.AddRequest`:

```go
type AddRequest struct {
    ID         string
    Kind       string
    Project    string
    Source     string
    SourceType string
    Reviewed   *bool
    Summary    string
    Body       string
    Now        time.Time
}
```

Use `*bool` in the request so update semantics can distinguish “unset” from “set false”.

Update semantics should preserve metadata when omitted, unlike current project/source behavior.

## Explorer Integration

After metadata lands, `explore` should support:

- filter by source type.
- filter reviewed/unreviewed.
- mark selected memory reviewed.
- mark selected memory unreviewed.
- show a badge for generated/unreviewed memories.

## MCP/CLI Integration

Add filters later to search/list commands:

```sh
agent-memoryd search --source-type transcript --reviewed=false "..."
agent-memoryd list-recent --reviewed=false
```

For MCP, avoid adding too many filter fields immediately unless agents need them. Start with `list_recent(reviewed?, source_type?)` after the listing tool exists.

## Acceptance Criteria

- New records include correct `source_type` for CLI, MCP, reflect, transcript, git, and import paths.
- Existing records without metadata continue to load and search.
- Updating a record preserves source type and reviewed state unless explicitly changed.
- Explorer can show reviewed/unreviewed status after UI phase lands.

## Tests

- Record creation defaults for each producer.
- Update semantics preserve omitted metadata.
- JSONL backward compatibility with old records.
- Import preserves existing metadata if present, otherwise sets `source_type=import`.

## Open Questions

- Should manual MCP `add` default to reviewed or unreviewed?
- Should generated memories be excluded from default search until reviewed? Recommendation: no, but make unreviewed visible.
- Should source type be free-form like `kind`, or constrained? Recommendation: free-form internally, documented common values.
