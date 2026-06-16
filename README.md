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

Initialize local config:

```sh
agent-memoryd init
```

Inspect the system, including command help and every managed resource created by
`init`:

```sh
agent-memoryd status
```

Run the MCP server over stdio:

```sh
agent-memoryd mcp
```

Run the resident worker:

```sh
agent-memoryd daemon
```

Generate a launchd plist:

```sh
agent-memoryd launchd-plist --bin "$(command -v agent-memoryd)"
```

Queue a git summary event:

```sh
agent-memoryd enqueue-git --repo "$(git rev-parse --show-toplevel)" --sha "$(git rev-parse HEAD)"
```

Remove the managed data root, config, manifest, store, index, spool, logs, and
LaunchAgent plist if present:

```sh
agent-memoryd uninstall --yes
```

The default build keeps source records in a local JSONL file and uses a small
lexical search fallback so contributors can build and test without native zvec
libraries.

The production retrieval index will use
[`github.com/zvec-ai/zvec-go`](https://github.com/zvec-ai/zvec-go). That SDK uses
cgo and native zvec libraries, so it will be integrated behind a narrow index
adapter instead of being required for the basic contributor test loop:

```sh
mise run zvec-libs
mise run build-zvec
```

## Architecture

`agent-memoryd` has four layers:

- Source store: a rebuildable JSONL memory log under `AGENT_MEMORYD_HOME`.
- Index: lexical by default, zvec-go behind the `zvec` build tag.
- Ingest: daemon polling for idle transcript JSONL files and git spool events.
- Retrieval: MCP tools and CLI commands share the same store.

The daemon watches configured transcript roots, waits until a transcript is idle,
then creates or updates a `session` memory. Git hooks do not summarize inline;
they enqueue a small event file, and the daemon turns that into a `git-summary`
memory out of band.

`agent-memoryd init` writes a resource manifest to the data root. `status` reads
that manifest and reports whether each managed path exists. `uninstall --yes`
uses the same manifest to tear down the local system resources it owns.

## MCP Tools

`search(query, project?, kind?, limit?)`

Returns matching memory summaries and ids.

`get(id)`

Returns one full memory.

`add(body, summary?, kind?, project?, source?, id?)`

Creates or updates a memory.

`forget(id)`

Deletes a memory from the local source store and derived index.
