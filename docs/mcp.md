# MCP

`memoryd mcp` runs the agent-memoryd stdio MCP server. Configure your MCP client to launch the binary with the `mcp` argument.

```json
{
  "command": "/absolute/path/to/memoryd",
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

Agents should treat memory retrieval as progressive disclosure: call `search` for compact summaries, then call `get` only for the ids that are relevant to the current task. Agents may call `add` and `forget` when they need to maintain durable memories during normal work. Agents should call `reflect` near the end of a meaningful session to extract durable preferences, instructions, project decisions, or facts learned during the session.
