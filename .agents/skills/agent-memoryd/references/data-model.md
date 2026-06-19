# Data model and store semantics

Table of contents
- Record schema
- Kinds
- IDs and upsert
- Store and write semantics
- Search and reindex
- Gotchas

## Record schema

The durable store is a zvec collection under `$MEMORYD_HOME/zvec`. Each record:

| field | notes |
|---|---|
| `id` | stable id; 8-byte random hex if not supplied |
| `kind` | free-form string; defaults to `fact` on direct add |
| `project` | optional scope string (omitted from JSON when empty) |
| `source` | optional source pointer (file path, `repo@sha`, transcript path) |
| `summary` | short line shown in search results; derived from body if omitted |
| `body` | full memory text; stored **verbatim** on direct `add` |
| `created_at`, `updated_at` | RFC3339 UTC timestamps |

Body handling on add: leading/trailing whitespace is trimmed; an empty body is rejected (`ErrEmptyBody`). If `summary` is empty it is derived from the body: whitespace-collapsed, truncated to 180 chars (CLI/MCP add) with a trailing `...`.

## Kinds

`kind` is not an enum — any string is accepted. Conventions seen in practice:
- `fact` — default for direct `add`; durable factual knowledge.
- `note` — used by the importer and the bundled import script (one memory per file).
- `feedback`, `preference`, `instruction` — guidance from the user about how to work.
- `reflection` — default kind for `reflect`-generated memories when the summarizer does not set one.
- `session`, `git-summary` — daemon-ingested transcript/commit reflections.

Use a consistent `kind` for a batch so it can be filtered (`search --kind <k>`) or swept later. Search filters `kind` and `project` by **exact match**.

## IDs and upsert

`add` is an upsert keyed by `id`:
- Supply a stable `id` → the matching record is updated in place (`created_at` preserved, `updated_at` refreshed). Re-running the same add is idempotent.
- Omit `id` → a new random id is generated every call (so omitting id while looping creates duplicates).

ID schemes used by the system:
- Direct add: random 8-byte hex, or whatever stable id you pass.
- `init --import` (markdown/text/jsonl): `import:<sha1(path[,line])>` (16 hex chars).
- `reflect` (session text): `reflect:<sha1(source+session)>:NN`.
- Daemon transcript/git reflections: `session:<hash>:NN`, `git:<hash>:NN`.

Pick a stable id namespace (e.g. `note:<slug>`) for any batch you might re-import or clean up.

## Store and write semantics

Defined in `internal/memory/store.go`:
- zvec takes an exclusive collection lock at open. The daemon is the only process that opens the store.
- CLI commands and the stdio MCP server route `Add`, `Get`, `Forget`, `Search`, `List`, `RebuildIndex`, and `Status` over the daemon Unix socket.
- The daemon serializes store access internally with a mutex, so concurrent CLI/MCP callers do not open or mutate zvec directly.
- If the daemon is not running, store operations fail with a daemon-not-running error instead of falling back to a direct open.

zvec is the **durable source of truth**. A legacy `memories.jsonl` in the data root is imported once on first store open and renamed `memories.jsonl.migrated`.

## Search and reindex

- Search runs a full-text leg and, when an embedder is configured and usable, a vector leg. The result lists are blended in Go with `search_fts_weight` and `search_vector_weight`.
- Embedding on write is best-effort. Records without vectors remain full-text searchable.
- `reindex` backfills embeddings for records whose vector field is null or missing. It does not rebuild from a JSONL source.

## Gotchas

- **Bullet-leading bodies break CLI add.** A body starting with `-` (markdown bullet) is parsed as a flag by cobra: `unknown shorthand flag: ' '`. Pass the body after a `--` sentinel and use `--flag=value` form. See [bulk-import.md](bulk-import.md).
- **Omitting `id` in a loop duplicates.** Always pass a stable `id` for idempotent batches.
- **`uninstall --yes` deletes the whole `root` directory** (including `zvec/`). Keep `root` dedicated to memoryd; back up before destructive ops.
- **CLI output is JSON** on stdout (pretty-printed). Pipe through `jq`/`python3` to extract fields.
