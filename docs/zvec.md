# zvec

`agent-memoryd` uses [`github.com/zvec-ai/zvec-go`](https://github.com/zvec-ai/zvec-go) as its sole store backend. zvec provides full-text search (FTS), vector search, and durable WAL-backed storage in a single collection.

## Build

Download native zvec libraries before building:

```sh
mise run zvec-libs
```

This fetches prebuilt libraries into `./lib/` for the current platform. Build is always cgo:

```sh
mise run build
```

The binary is written to `./memoryd`. There is no pure-Go fallback build. The native library is required.

## Store Layout

The zvec collection lives at `$MEMORYD_HOME/zvec`. Each record is stored as a zvec document with string fields (`kind`, `project`, `source`, `summary`, `body`, `created_at`, `updated_at`) and an optional `embedding` vector field.

The embedding field is nullable. Records without an embedding are still stored and fully searchable via FTS. Run `reindex` to backfill embeddings for records that were stored before an embedder was configured.

## Prebuilt Native Libraries

Supported platforms for prebuilt libraries:

- macOS arm64 (`darwin_arm64`)
- Linux amd64 (`linux_amd64`)
- Linux arm64 (`linux_arm64`)

The `./lib/` directory is gitignored. Run `mise run zvec-libs` after each checkout to populate it.

## Runtime Library Location

When running `mise run build`, the binary embeds an rpath pointing at the working-tree `./lib/` directory. This is suitable for development.

When running `mise run install-local`, the binary is rebuilt with an rpath pointing at `~/.local/lib/memoryd/`, and the native library is copied there. This makes the installed binary independent of the repository checkout location.

## Tokenizer

FTS uses the `standard` tokenizer. This tokenizer lowercases and splits on whitespace and punctuation. It covers most code identifiers and natural language well enough for local memory search.
