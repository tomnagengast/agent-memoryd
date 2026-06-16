# agent-memoryd

Local memory daemon for coding agents.

`agent-memoryd` is a small local service and MCP server that lets coding agents search, fetch, add, and forget durable memories. The project is intended for fresh installs and public contribution, not migration from any one person's existing memory setup.

## Quick Start

```sh
mise install
mise run build
./agent-memoryd --help
./agent-memoryd init
./agent-memoryd status
```

Run the MCP server over stdio:

```sh
./agent-memoryd mcp
```

Run the resident ingest worker:

```sh
./agent-memoryd daemon
```

Daemon transcript and git producers require a configured `summarizer_command`. The default uses `codex exec` in read-only ephemeral mode.

Add and retrieve a memory from the CLI:

```sh
./agent-memoryd add --project example --summary "Uses local memory" \
  "agent-memoryd stores durable local memories for coding agents."
./agent-memoryd search --project example "local memory"
```

## Goals

- Local-first memory store for coding agents
- MCP tools for `search`, `get`, `add`, `forget`, and `reflect`
- Agent-managed memories without burning the agent's main turn on note writing
- Summarizer-driven transcript and git producers that store distilled memories with source pointers
- Rebuildable source records with zvec-backed retrieval

## Non-Goals

- Migrating a private markdown memory system
- Requiring a hosted service
- Requiring users to adopt a specific coding-agent harness

## Commands

`init` creates the managed data root, config, memory store, git spool, logs directory, and resource manifest.

`status` prints system help, MCP tool help, loaded config, store status, and every resource persisted by `init`.

`help` and `--help` show command help. `completion` generates shell completion scripts.

`mcp` runs the stdio MCP server.

`daemon` runs the resident ingest worker. `scan-once` runs one ingest pass.

`add`, `search`, `get`, and `forget` manage memories from the CLI.

`enqueue-git` queues a git event for the daemon to summarize later.

`launchd-plist` renders a macOS LaunchAgent plist to stdout.

`reindex` rebuilds the configured retrieval index from `memories.jsonl`.

`uninstall --yes` removes managed `agent-memoryd` resources.

## Docs

- [Docs index](./docs/README.md)
- [Install](./docs/install.md)
- [Getting started](./docs/getting-started.md)
- [Config](./docs/config.md)
- [Architecture](./docs/architecture.md)
- [MCP](./docs/mcp.md)
- [Daemon](./docs/daemon.md)
- [Git hooks](./docs/git-hooks.md)
- [zvec](./docs/zvec.md)
- [Uninstall](./docs/uninstall.md)
- [Contributing](./docs/contributing.md)

## Development

This project uses mise for tool and task management.

```sh
mise install
mise run test
mise run build
```

The default build keeps source records in a local JSONL file and uses a small lexical search fallback so contributors can build and test without native zvec libraries.

The production retrieval index uses [`github.com/zvec-ai/zvec-go`](https://github.com/zvec-ai/zvec-go) behind the `zvec` build tag. That SDK uses cgo and native zvec libraries, so it is not required for the basic contributor test loop:

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

The daemon polls configured transcript roots, waits until a transcript is idle, then passes the transcript plus existing memory summaries to the configured summarizer. Git hooks do not summarize inline; they enqueue a small event file, and the daemon passes `git show` output plus existing memory summaries to the same summarizer. The MCP `reflect` tool uses the same summarizer path for the current session. These producers store distilled memories with transcript, session, or commit references, not raw logs.

`agent-memoryd init` writes a resource manifest to the data root. `status` reads that manifest and reports whether each managed path exists. `uninstall --yes` uses the same manifest to tear down the local system resources it owns.

See [docs/architecture.md](./docs/architecture.md) for more detail.

## MCP Tools

`search(query, project?, kind?, limit?)`

Returns matching memory summaries and ids.

`get(id)`

Returns one full memory.

`add(body, summary?, kind?, project?, source?, id?)`

Creates or updates a memory.

`forget(id)`

Deletes a memory from the local source store and derived index.

`reflect(session?, transcript_path?, project?, cwd?, source?, limit?)`

Extracts durable memories from the current session. If `session` is provided, the tool summarizes that text. Otherwise it uses `transcript_path`, or the newest configured transcript if no path is provided.

See [docs/mcp.md](./docs/mcp.md) for MCP configuration and schemas.
