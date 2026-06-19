# MCP

`memoryd mcp` runs the agent-memoryd stdio MCP server. Configure your MCP client to launch the binary with the `mcp` argument.

```json
{
  "command": "/absolute/path/to/memoryd",
  "args": ["mcp"],
  "env": {
    "MEMORYD_HOME": "/Users/you/.local/share/memoryd"
  }
}
```

## Store Access

The MCP server connects to the daemon's Unix socket at `$MEMORYD_HOME/memoryd.sock` and routes all store operations through the daemon. The daemon must be running; MCP never opens the zvec collection directly.

## Tools

### status

Return compact store and embedder health without the CLI-only config, service, hook, or resource details.

Input:

```json
{}
```

Output:

```json
{
  "path": "/Users/you/.local/share/memoryd/zvec",
  "backend": "zvec",
  "memory_count": 42,
  "pending_embedding": 0,
  "embedder": {
    "configured": true,
    "ok": true,
    "dimension": 768
  }
}
```

### context

Search local memories and expand the top hits into concise context. Use this when an agent needs the likely-relevant memory bodies without manually calling `search` and then `get` for each result.

Input:

```json
{
  "query": "preferred test command",
  "project": "optional-project",
  "kind": "optional-kind",
  "limit": 5,
  "max_chars": 6000
}
```

`limit` defaults to 5 and is capped at 20. `max_chars` is the total body-character budget across expanded memories; it defaults to 6000 and is capped at 20000.

Output:

```json
{
  "max_chars": 6000,
  "truncated": false,
  "results": [
    {
      "id": "memory-id",
      "kind": "fact",
      "project": "optional-project",
      "source": "optional-source",
      "summary": "short summary",
      "body": "full memory body or a bounded excerpt",
      "body_truncated": false,
      "score": 1.25
    }
  ]
}
```

### search

Search local memory summaries.

Input:

```json
{
  "query": "preferred test command",
  "project": "optional-project",
  "kind": "optional-kind",
  "limit": 5,
  "diagnostics": false
}
```

Output:

```json
{
  "results": [
    {
      "id": "memory-id",
      "kind": "fact",
      "project": "optional-project",
      "source": "optional-source",
      "summary": "short summary",
      "score": 1.25
    }
  ]
}
```

Set `diagnostics` to `true` to include search execution metadata:

```json
{
  "results": [],
  "diagnostics": {
    "embedder_used": true,
    "fts_hits": 3,
    "vector_hits": 5,
    "query_embedding_dimension": 768
  }
}
```

### get

Fetch one full memory by id.

Input:

```json
{
  "id": "memory-id"
}
```

Output:

```json
{
  "found": true,
  "memory": {
    "id": "memory-id",
    "kind": "fact",
    "project": "optional-project",
    "source": "optional-source",
    "summary": "short summary",
    "body": "full memory body",
    "created_at": "2026-06-16T12:00:00Z",
    "updated_at": "2026-06-16T12:00:00Z"
  }
}
```

If no memory exists, `found` is `false`.

### add

Create or update a durable local memory. Direct `add` stores the supplied body verbatim; summarization is only used by daemon producers.

Input:

```json
{
  "id": "optional-stable-id",
  "kind": "fact",
  "project": "optional-project",
  "source": "optional-source",
  "summary": "short summary",
  "body": "full memory body"
}
```

If `id` is omitted, `memoryd` generates one. If `summary` is omitted, it is derived from the body.

### forget

Delete a local memory by id.

Input:

```json
{
  "id": "memory-id"
}
```

Output:

```json
{
  "ok": true,
  "id": "memory-id"
}
```

If no memory exists, `ok` is `false`.

### reflect

Extract durable memories from the current session through the configured summarizer.

Input with explicit session text:

```json
{
  "project": "optional-project",
  "cwd": "/optional/current/working/directory",
  "source": "optional-session-reference",
  "session": "current session text or transcript excerpt",
  "limit": 12
}
```

Input with a transcript file:

```json
{
  "transcript_path": "/path/to/session.jsonl",
  "project": "optional-project"
}
```

If both `session` and `transcript_path` are omitted, `reflect` uses the newest `.jsonl` transcript under the configured `transcript_roots`.

Output:

```json
{
  "ok": true,
  "source": "session-reference-or-transcript-path",
  "memories": [
    {
      "id": "memory-id",
      "kind": "preference",
      "project": "optional-project",
      "source": "session-reference-or-transcript-path",
      "summary": "short summary",
      "body": "distilled memory body with a More detail reference",
      "created_at": "2026-06-16T12:00:00Z",
      "updated_at": "2026-06-16T12:00:00Z"
    }
  ]
}
```

`reflect` does not store supplied session text or raw transcript content directly. It sends source material plus existing memory summaries to the configured `summarizer_command`, then stores only the returned distilled memories.

## Usage Pattern

Agents should usually call `context` when they need concise memory context for the current task. For tighter control, use progressive disclosure manually: call `search` for compact summaries, then call `get` only for the ids that are relevant. Agents may call `add` and `forget` when they need to maintain durable memories during normal work. Agents should call `reflect` near the end of a meaningful session to extract durable preferences, instructions, project decisions, or facts learned during the session.
