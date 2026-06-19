# MCP tools

`memoryd mcp` runs a stdio MCP server (server name `agent-memoryd`) exposing five tools. CLI and MCP share the same store code, so anything below mirrors the CLI behavior in [cli.md](cli.md) and the model in [data-model.md](data-model.md).

## Client config

```json
{
  "mcpServers": {
    "agent-memoryd": {
      "type": "stdio",
      "command": "memoryd",
      "args": ["mcp"],
      "env": { "MEMORYD_HOME": "/Users/you/.local/share/memoryd" }
    }
  }
}
```

`MEMORYD_HOME` is optional; without it the server uses the default root. The tool resolves the same `config.json`/store as the CLI.

## Tools

### search(query, project?, kind?, limit?)
Returns `{ results: [{id, kind, project, source, summary, score}] }`. `project`/`kind` filter by exact match; default `limit` 5 (cap 50). Call this first.

### get(id)
Returns `{ found, memory }` where `memory` is the full record, or `{ found: false }`.

### add(body, summary?, kind?, project?, source?, id?)
Creates or updates a memory; body stored verbatim. Returns `{ ok, memory }`. Supply a stable `id` to upsert; omit it for a new random id. Unlike the CLI, MCP passes the body as a JSON field, so there is **no `--` dash gotcha** here — prefer MCP `add` from an agent when a body might start with `-`.

### forget(id)
Returns `{ ok, id }` (`ok:false` if not found). Removes from store and index.

### reflect(session?, transcript_path?, project?, cwd?, source?, limit?)
Distills durable memories from session material via the configured `summarizer_command` (see [daemon.md](daemon.md)). Resolution order:
1. `session` text supplied → summarize it directly (id `reflect:<hash>:NN`, default kind `reflection`).
2. else `transcript_path` → read and summarize that transcript.
3. else newest `.jsonl` under `transcript_roots`.

`project`/`cwd` scope the result (`cwd` infers project from its basename). `limit` caps how many existing memory summaries are sent as dedup context (default `memory_context_limit`, 12). Stored memories carry a `source` pointer and a `More detail:` reference — raw session/transcript text is **not** stored. Returns `{ ok, source, memories }`.

## Retrieval pattern (progressive disclosure)

1. `search` for compact summaries + ids — keeps the turn small.
2. `get` only the ids relevant to the task to pull full bodies.
3. `add`/`forget` to maintain durable memories during work (use a stable `id` and a consistent `kind`).
4. `reflect` near the end of a meaningful session to capture durable preferences, instructions, decisions, and facts.

## Note on long-lived MCP servers

Each tool call reads the store file fresh, so a running MCP server reflects external writes (CLI add, daemon, the bundled import script) without a restart. If a client appears to show stale results, it is almost always the client caching a previous tool result — reconnect the MCP server (e.g. `/mcp` in Claude Code) to be sure.
