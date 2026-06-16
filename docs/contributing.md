# Contributing

`agent-memoryd` is intended to be understandable to new contributors. Keep changes small, local-first, and explicit about what data is persisted.

## Development Loop

Install tools:

```sh
mise install
```

Run the standard checks:

```sh
mise run fmt
mise run test
mise run build
```

For zvec-specific changes, also run:

```sh
mise run zvec-libs
mise run build-zvec
```

## Project Shape

The CLI lives in `internal/app`.

The durable memory model and source store live in `internal/memory`.

Config and lifecycle resources live in `internal/config`.

Daemon ingestion lives in `internal/daemon`, `internal/ingest`, and `internal/spool`.

Index adapters live in `internal/indexer` and `internal/zvecindex`.

## Design Principles

Keep `memories.jsonl` as the durable source of truth. Indexes should be rebuildable.

Keep MCP tools compact. `search` should return summaries and ids; `get` should return full records only when needed.

Do not add migration code from a private memory system. Public contributors should see a clean install path.

Prefer local behavior over hosted services. External dependencies should be optional unless they are core to retrieval.

## Documentation

Update `README.md` and the relevant file in `docs/` when changing commands, config fields, managed resources, MCP tools, ingestion behavior, or zvec setup.

Docs should describe current behavior plainly. If something is a future direction, mark it as such or leave it out.
