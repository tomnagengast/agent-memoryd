# agent-memoryd

Local memory daemon for coding agents.

`agent-memoryd` is a small local service and MCP server that lets coding agents
search, fetch, add, and forget durable memories. The project is intended for
fresh installs and public contribution, not migration from any one person's
existing memory setup.

## Goals

- Local-first memory store for coding agents
- MCP tools for `search`, `get`, `add`, and `forget`
- Agent-managed memories without burning the agent's main turn on note writing
- Optional git-history summarization through thin hook events
- Rebuildable source records with zvec-backed retrieval

## Non-Goals

- Migrating a private markdown memory system
- Requiring a hosted service
- Requiring users to adopt a specific coding-agent harness

## Development

This project uses mise for tool and task management.

```sh
mise install
mise run test
mise run build
```

The first implementation keeps source records in a local JSONL file and uses a
small lexical search fallback so the project can be built and tested without
native zvec libraries.

The production retrieval index will use
[`github.com/zvec-ai/zvec-go`](https://github.com/zvec-ai/zvec-go). That SDK uses
cgo and native zvec libraries, so it will be integrated behind a narrow index
adapter instead of being required for the basic contributor test loop.

## MCP Tools

`search(query, project?, kind?, limit?)`

Returns matching memory summaries and ids.

`get(id)`

Returns one full memory.

`add(body, summary?, kind?, project?, source?, id?)`

Creates or updates a memory.

`forget(id)`

Deletes a memory from the local source store and derived index.
