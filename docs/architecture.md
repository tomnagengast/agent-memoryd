# Architecture

`agent-memoryd` is a local-first memory service for coding agents. It keeps memories in a single zvec-backed store on disk and exposes the same memory operations through CLI commands, an MCP server, and a resident daemon.

## Layers

The store is a zvec collection at `$MEMORYD_HOME/zvec`. Each record has an id, kind, optional project, optional source reference, summary, body, and timestamps. zvec is the sole durable store; there is no separate JSONL source of truth.

On first open, if a legacy `memories.jsonl` file is present in the same root, it is imported once into the zvec collection and then renamed `memories.jsonl.migrated`. No further migration is needed.

The daemon ingests two local input streams: idle transcript JSONL files and git event files. These producers pass source material plus existing memory summaries to the configured summarizer, then store the distilled memories returned by that agent with source references for progressive disclosure.

The MCP server exposes `status`, `context`, `search`, `get`, `add`, `forget`, and `reflect` over stdio. The CLI commands use the same store interface as the MCP tools.

## Single-Owner + IPC Concurrency Model

zvec takes an exclusive directory lock at open time. Only one process can hold the collection at once. `agent-memoryd` handles this through a single-owner model:

The daemon holds the zvec collection exclusively and serves all store operations (from the CLI, MCP server, and its own ingest loop) over a Unix socket at `$MEMORYD_HOME/memoryd.sock`. The daemon serializes all collection access internally with a mutex. CLI commands and MCP never open zvec directly; if the daemon socket is unavailable, store operations fail with a daemon-not-running error.

This design means write safety for simultaneous writers comes from routing through the single owning process. Do not rely on concurrent direct opens.

## Retrieval Flow

Agents can call `context` to search and expand the top memory hits into bounded body excerpts in one step. For manual progressive disclosure, agents can call `search` first; search returns summaries and ids, which keeps most turns compact. The agent should call `get` only when a full memory is needed.

Search is hybrid: it runs a full-text search (FTS) leg using zvec's `standard` tokenizer and a vector search leg using the configured embedder, then blends the two ranked lists in Go using configurable weights (`search_fts_weight`, `search_vector_weight`). When no embedder is configured or the embedder fails, only the FTS leg runs. The first-class embedder provider is Ollama via `/api/embed`; `embedder_command` remains as an escape hatch.

Embedding on write is best-effort. If no embedder is configured or the embedding call fails, the vector field is stored as null and the record is still persisted and full-text searchable. `reindex` backfills embeddings for records with null vectors. `status` reports `pending_embedding` (records without vectors) and an `embedder` probe with `configured`, `ok`, and `dimension` fields.

Manual or agent-managed updates use `add`. If an id is supplied, `add` updates that stable record. If no id is supplied, a new id is generated. Direct adds store the supplied body verbatim.

Daemon-generated updates and MCP `reflect` use the summarizer. Transcript, git, and reflection producers provide raw source material to the summarizer, but the store receives only the generated memory body plus a `source` pointer and `More detail:` reference.

Deletion uses `forget`. The record is removed from the store.

## Lifecycle Flow

`init` creates the managed data root and records the resources it owns in `resources.json`. On a new interactive install it walks through fresh-vs-import setup, default transcript ingestion roots, Ollama semantic search, and daemon startup. Scripted installs can use `--fresh`, `--import <path>`, or `--no-daemon`. After memory setup, `init` writes managed global Git hooks and sets global `core.hooksPath` when that value is unset or already points at the managed hook directory. On macOS it also writes the standard LaunchAgent plist, bootstraps it with launchd, and kickstarts the daemon unless skipped. `init --no-daemon` skips only that service setup.

`status` reads the manifest and reports command help, MCP tool help, config, store status, Git hook status, and whether every managed path exists.

`uninstall --yes` uses the manifest to remove the managed data root and the standard macOS LaunchAgent plist path if present. It also unsets global `core.hooksPath` when that setting points at the managed hook directory.

## Current Boundaries

The daemon polls configured paths instead of using native file system events. The MCP server is stdio only. The `launchd-plist` command renders a plist to stdout for inspection or advanced manual installs.
