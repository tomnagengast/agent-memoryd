# MCP

`agent-memoryd mcp` runs a stdio MCP server. Configure your MCP client to launch the binary with the `mcp` argument.

```json
{
  "command": "/absolute/path/to/agent-memoryd",
  "args": ["mcp"],
  "env": {
    "AGENT_MEMORYD_HOME": "/Users/you/.local/share/agent-memoryd"
  }
}
```

## Tools

### search

Search local memory summaries.

Input:

```json
{
  "query": "preferred test command",
  "project": "optional-project",
  "kind": "optional-kind",
  "limit": 5
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

If `id` is omitted, `agent-memoryd` generates one. If `summary` is omitted, it is derived from the body.

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

## Usage Pattern

Agents should treat memory retrieval as progressive disclosure: call `search` for compact summaries, then call `get` only for the ids that are relevant to the current task. Agents may call `add` and `forget` when they need to maintain durable memories during normal work.
