# Data model and store semantics

Table of contents
- Record schema
- Kinds
- IDs and upsert
- Store file and write semantics
- Index and reindex
- Gotchas

## Record schema

The source store is a JSONL file (`memories.jsonl`); one JSON object per line. Each record:

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

## Store file and write semantics

Defined in `internal/memory/store.go`:
- Every operation (`Add`, `Get`, `Forget`, `Search`, `List`, `RebuildIndex`, `Status`) reads the **entire file fresh** (`readLocked`) — there is no long-lived in-memory cache of records. So CLI, MCP, and daemon processes all see current disk state per call.
- `Add`/`Forget` do read-modify-write: read all records, mutate the map by id, then write **all** records to `memories.jsonl.tmp` and atomically `rename` over the store. Records are sorted by `created_at` on write.
- Concurrency: writes are serialized only by an in-process mutex. There is **no cross-process file lock**. Because every writer reads the full file first and writes the union, concurrent writers do not clobber each other's existing records — but two writes that read the same baseline and rename in a tight race can drop one side's single new record. Mitigations: prefer sequential writes; rely on idempotent stable ids and re-run to converge.

The store is the **rebuildable source of truth**. The retrieval index is derived data.

## Index and reindex

- `index_backend` is `lexical` by default (pure Go, no native deps). A binary built with the `zvec` tag (`mise run build-zvec`) uses vector retrieval.
- The lexical index scores by fraction of query tokens found in `summary + body + kind + project` (tokenized on `[a-z0-9]`). `source` is **not** scored. Default limit 5, capped at 50.
- `add`/`forget` keep the index in sync incrementally. After editing `memories.jsonl` by hand or via a process that bypassed the index, run `agent-memoryd reindex` to rebuild it from the store.

## Gotchas

- **Bullet-leading bodies break CLI add.** A body starting with `-` (markdown bullet) is parsed as a flag by cobra: `unknown shorthand flag: ' '`. Pass the body after a `--` sentinel and use `--flag=value` form. See [bulk-import.md](bulk-import.md).
- **Omitting `id` in a loop duplicates.** Always pass a stable `id` for idempotent batches.
- **`uninstall --yes` deletes the whole `root` directory** (including `memories.jsonl`). Keep `root` dedicated to agent-memoryd; back up `memories.jsonl` before destructive ops.
- **CLI output is JSON** on stdout (pretty-printed). Pipe through `jq`/`python3` to extract fields.
