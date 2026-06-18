# Contributing

`agent-memoryd` is intended to be understandable to new contributors. Keep changes small, local-first, and explicit about what data is persisted.

## Development Loop

Install tools:

```sh
mise install
```

Download the native zvec libraries (required before building or running full tests):

```sh
mise run zvec-libs
```

Run the standard checks:

```sh
mise run fmt
mise run test
mise run build
```

GitHub Actions runs formatting, tests, and the build on pushes to `main` and pull requests.

Create a GitHub release by pushing a semver tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

## Project Shape

The CLI lives in `internal/app`.

The durable memory model and zvec-backed store live in `internal/memory`.

Config and lifecycle resources live in `internal/config`.

Daemon ingestion lives in `internal/daemon`, `internal/ingest`, and `internal/spool`.

The configurable summarization adapter lives in `internal/summarizer`.

The configurable embedding adapter lives in `internal/embedder`.

The advisory file lock helper lives in `internal/flock`.

The IPC server and client (daemon socket) live in `internal/storerpc`.

## Build Details

`mise run build` always uses `CGO_ENABLED=1` and links against the zvec native library in `./lib/`. There is no pure-Go fallback build. `mise run zvec-libs` must be run before `mise run build`.

`mise run install-local` builds the binary with an rpath pointing at `~/.local/lib/agent-memoryd/` (not the working tree `./lib/` directory) and copies the native library there before installing the binary to `~/.local/bin/`. This makes the installed binary independent of the repository checkout location.

## Design Principles

Keep the zvec store as the sole durable memory store. The store is the system of record, not a derived cache.

Keep MCP tools compact. `search` should return summaries and ids; `get` should return full records only when needed. `reflect` should store distilled memories through the summarizer, not raw session text.

Do not add migration code from a private memory system. Public contributors should see a clean install path.

Prefer local behavior over hosted services. External dependencies should be optional unless they are core to retrieval.

Daemon and MCP reflection producers should not store raw transcripts, session text, tool logs, diffs, or git output as memory bodies. Pass source material to the summarizer and store only distilled memories with `source` and `More detail:` references.

## Documentation

Update `README.md` and the relevant file in `docs/` when changing commands, config fields, managed resources, MCP tools, ingestion behavior, summarizer behavior, or embedder/search behavior.

Docs should describe current behavior plainly. If something is a future direction, mark it as such or leave it out.
