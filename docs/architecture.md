# Architecture

`agent-memoryd` is a local-first memory service for coding agents. It keeps a small source store on disk, builds a local retrieval index, and exposes the same memory operations through CLI commands and MCP tools.

## Layers

The source store is `memories.jsonl`. Each record has an id, kind, optional project, optional source reference, summary, body, and timestamps. This file is the durable source of truth.

The index is derived data. The default build uses a pure-Go lexical index. A binary built with the `zvec` build tag can use `github.com/zvec-ai/zvec-go` for vector retrieval.

The daemon ingests two local input streams: idle transcript JSONL files and git event files. Transcript ingestion writes `session` memories. Git event ingestion writes `git-summary` memories after reading the commit with `git show`.

The MCP server exposes `search`, `get`, `add`, and `forget` over stdio. The CLI commands call the same store code as the MCP tools.

## Retrieval Flow

Agents should call `search` first. Search returns summaries and ids, which keeps most turns compact. The agent should call `get` only when a full memory is needed.

Manual or agent-managed updates use `add`. If an id is supplied, `add` updates that stable record. If no id is supplied, a new id is generated.

Deletion uses `forget`. The record is removed from the source store and the derived index is updated.

## Lifecycle Flow

`init` creates the managed data root and records the resources it owns in `resources.json`.

`status` reads the manifest and reports command help, MCP tool help, config, store status, and whether every managed path exists.

`uninstall --yes` uses the manifest to remove the managed data root and the standard macOS LaunchAgent plist path if present.

## Current Boundaries

The daemon polls configured paths instead of using native file system events. The MCP server is stdio only. The launchd command renders a plist to stdout; it does not install or load the service automatically.
