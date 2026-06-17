# MCP And CLI Status/List Tools

> updated: 2026-06-17

## Goal

Make the MCP and CLI surfaces better for daily use by agents and humans. Add low-risk observability and listing tools before expanding more complex memory behavior.

## Problem

The current MCP surface is minimal and agent-friendly, but it lacks health and navigation affordances.

Existing MCP tools:

- `search` at `internal/app/mcp.go:86`
- `get` at `internal/app/mcp.go:102`
- `add` at `internal/app/mcp.go:116`
- `forget` at `internal/app/mcp.go:134`
- `reflect` at `internal/app/mcp.go:148`

Related concerns:

- MCP server version is hardcoded to `0.1.0`: `internal/app/mcp.go:81`.
- `status` exists only as a CLI command: `internal/app/app.go:167`.
- `status` output is broad but not optimized for quick agent health checks: `internal/app/app.go:457`.
- There is no `list_recent`, `list_projects`, or `list_kinds` tool.

## Proposed MCP Tools

Add:

```text
status()
list_recent(project?, kind?, limit?)
list_projects()
list_kinds(project?)
```

Optional later:

```text
search_diagnostics(query, project?, kind?)
```

## Proposed CLI Commands

Mirror the useful listing surfaces:

```sh
agent-memoryd list-recent [--project <name>] [--kind <kind>] [--limit 20]
agent-memoryd list-projects
agent-memoryd list-kinds [--project <name>]
```

Keep `status` as the full diagnostic command, but consider adding:

```sh
agent-memoryd status --brief
agent-memoryd status --json
```

The command already emits JSON, so `--json` may be a no-op or unnecessary. `--brief` is the useful addition.

## Output Shapes

`list_recent` MCP output:

```json
{
  "memories": [
    {
      "id": "...",
      "kind": "fact",
      "project": "agent-memoryd",
      "source": "...",
      "summary": "...",
      "created_at": "...",
      "updated_at": "..."
    }
  ]
}
```

`list_projects` output:

```json
{
  "projects": [
    {"name": "agent-memoryd", "memory_count": 42, "updated_at": "..."}
  ]
}
```

`status` MCP output should be compact:

```json
{
  "ok": true,
  "store": {"memory_count": 42, "path": "..."},
  "index": {"backend": "lexical", "stale": false},
  "daemon": {"configured": true, "running": true},
  "warnings": []
}
```

## Implementation Plan

1. Add pure helper functions for recent/project/kind aggregation over `store.List`.
2. Add CLI commands in `internal/app/app.go`.
3. Add MCP tool input/output structs in `internal/app/mcp.go`.
4. Replace MCP hardcoded version with `version.Version` or `version.String()` parsing.
5. Update `docs/mcp.md` and README command list.
6. Add tests for command output and MCP handler behavior.

## Acceptance Criteria

- Agents can call MCP `status` to know whether memory is usable.
- Agents can discover projects without guessing project names.
- Humans can list recent memories without opening the TUI.
- MCP server reports the real build/project version instead of hardcoded `0.1.0`.

## Tests

- Aggregation helpers for project counts and recency ordering.
- CLI JSON output tests for empty and populated stores.
- MCP tool handler tests for default limits and filters.

## Open Questions

- Should tool names remain generic (`status`) or become namespaced (`agent_memory_status`) to avoid collisions?
- Should `list_recent` return full bodies or summaries only? Recommendation: summaries only, preserve progressive disclosure.
- Should `status` expose absolute local paths over MCP by default?
