# agent-memoryd

Local memory daemon for coding agents.

`agent-memoryd` is a small local service and MCP server that lets coding agents search, fetch, add, and forget durable memories. The project is intended for fresh installs and public contribution, with an optional generic import path for existing JSONL, markdown, and text memories.

## Quick Start

```sh
mise install
mise run zvec-libs
mise run build
./memoryd --help
./memoryd --version
./memoryd init
./memoryd status
```

In an interactive terminal, `init` walks through onboarding choices: start fresh or import existing memories, enable default transcript ingestion roots, and start the daemon service now. Use `./memoryd init --fresh` for a non-interactive fresh install, or `./memoryd init --import ~/notes/agent` to import an existing JSONL file or markdown/text directory.

`init` also installs managed global Git hooks via `git config --global core.hooksPath` when no global hook path is already configured. On macOS, it installs and starts the managed launchd daemon. Use `./memoryd init --no-daemon` if you only want to skip the daemon service.

Run the MCP server over stdio:

```sh
./memoryd mcp
```

Run the resident ingest worker manually:

```sh
./memoryd daemon
```

Explore memories interactively:

```sh
./memoryd explore
```

Daemon transcript and git producers require a configured `summarizer_command`. The default uses `codex exec` in read-only ephemeral mode.

Add and retrieve a memory from the CLI:

```sh
./memoryd add --project example --summary "Uses local memory" \
  "agent-memoryd stores durable local memories for coding agents."
./memoryd search --project example "local memory"
```

## Goals

- Local-first memory store for coding agents
- MCP tools for `search`, `get`, `add`, `forget`, and `reflect`
- Agent-managed memories without burning the agent's main turn on note writing
- Summarizer-driven transcript and git producers that store distilled memories with source pointers
- zvec-backed retrieval with hybrid full-text and vector search

## Non-Goals

- Requiring migration from any specific private memory system
- Requiring a hosted service
- Requiring users to adopt a specific coding-agent harness

## Commands

`init` creates the managed data root, config, zvec store, git spool, global Git hook scripts, logs directory, and resource manifest. It can start fresh or import existing memories from an agent-memoryd JSONL file or a markdown/text file tree. It configures Git's global `core.hooksPath` when that setting is unset or already points at the managed hook directory. On macOS it also writes and starts the managed LaunchAgent unless `--no-daemon` is passed.

`status` prints system help, MCP tool help, loaded config, store status, launchd service status, and every resource persisted by `init`.

`help` and `--help` show command help. `--version` and `-v` print build metadata. `completion` generates shell completion scripts.

`mcp` runs the stdio MCP server.

`daemon` runs the resident ingest worker. `scan-once` runs one ingest pass.

`add`, `search`, `get`, and `forget` manage memories from the CLI.

`explore` opens an interactive memory browser with a live search bar, navigable result list, and full-memory detail pane.

`enqueue-git` queues a git event for the daemon to summarize later.

`launchd-plist` renders a macOS LaunchAgent plist to stdout for manual inspection or advanced installs.

`reindex` backfills vector embeddings for memories that were stored without an embedder configured.

`uninstall --yes` removes managed local memory resources.

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
mise run zvec-libs
mise run test
mise run build
```

`mise run build` always uses `CGO_ENABLED=1` and links the zvec native library. Run `mise run zvec-libs` first to populate `./lib/`. The build stamps the binary with `git describe --tags --always --dirty`, the short commit, and the UTC build time. Set `MEMORYD_VERSION` to override the displayed version for a release build.

Compare the checked-out binary with the installed one:

```sh
./memoryd --version
memoryd --version
```

Update the installed binary and native library with an atomic install:

```sh
mise run install-local
memoryd init
```

`mise run install-local` rebuilds the binary with an rpath pointing at `~/.local/lib/memoryd/` and copies the native library there, so the installed binary works independently of the repository working tree.

## Architecture

`agent-memoryd` has four layers:

- Store: a zvec-backed collection at `$MEMORYD_HOME/zvec`. All memories are stored here; no separate JSONL source of truth.
- Ingest: daemon polling for idle transcript JSONL files and git spool events.
- IPC: the daemon owns the store and serves CLI/MCP operations over a Unix socket. Store commands require the daemon to be running.
- Retrieval: hybrid full-text + vector search blended in Go. Embedding is best-effort; records without vectors are full-text searchable and backfilled by `reindex`.

The daemon polls configured transcript roots, waits until a transcript is idle, then passes the transcript plus existing memory summaries to the configured summarizer. Git hooks do not summarize inline; they enqueue a small event file, and the daemon passes `git show` output plus existing memory summaries to the same summarizer. The MCP `reflect` tool uses the same summarizer path for the current session. These producers store distilled memories with transcript, session, or commit references, not raw logs.

`memoryd init` writes a resource manifest to the data root, installs managed global Git hooks when safe, and starts the managed LaunchAgent on macOS. `status` reads that manifest and reports whether each managed path exists. `uninstall --yes` uses the same manifest to tear down the local system resources it owns.

See [docs/architecture.md](./docs/architecture.md) for more detail.

## MCP Tools

`search(query, project?, kind?, limit?)`

Returns matching memory summaries and ids.

`get(id)`

Returns one full memory.

`add(body, summary?, kind?, project?, source?, id?)`

Creates or updates a memory.

`forget(id)`

Deletes a memory from the local store.

`reflect(session?, transcript_path?, project?, cwd?, source?, limit?)`

Extracts durable memories from the current session. If `session` is provided, the tool summarizes that text. Otherwise it uses `transcript_path`, or the newest configured transcript if no path is provided.

See [docs/mcp.md](./docs/mcp.md) for MCP configuration and schemas.
