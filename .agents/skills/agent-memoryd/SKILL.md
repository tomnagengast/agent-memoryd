---
name: agent-memoryd
description: Local-first memory store and MCP server for coding agents — search, get, add, forget, and reflect over a zvec-backed store owned by the daemon, plus transcript and git ingestion into distilled memories. Use when operating the memory store from the CLI (memoryd add/search/get/forget/reindex) or its MCP tools; configuring or debugging the daemon, launchd service, git hooks, embedder, summarizer, or resource manifest; bulk-importing markdown/text/JSONL notes; reasoning about the record/store data model and its gotchas; or developing the agent-memoryd Go codebase. Triggers include the memoryd binary, agent-memoryd MCP server, the ~/.local/share/agent-memoryd data root, zvec store, AGENT_MEMORYD_HOME, or the tomnagengast/agent-memoryd repo.
---

# agent-memoryd

A small local service + MCP server that gives coding agents durable, searchable memories. One daemon owns the zvec store and serves CLI/MCP store operations over a Unix socket. The daemon also turns idle agent transcripts and git commits into distilled memories.

## Orientation

- Binary: `memoryd` (installed at `~/.local/bin/memoryd`; in-repo build is `./memoryd`).
- Data root: `$AGENT_MEMORYD_HOME` or `~/.local/share/agent-memoryd`. Holds `config.json`, `resources.json` (manifest), `spool/`, `git-hooks/`, `logs/`, `zvec/`, and the daemon socket when running.
- Interfaces: CLI subcommands and the stdio MCP server (`memoryd mcp`). Both read/write the same store.
- Repo: `github.com/tomnagengast/agent-memoryd`; Go, managed with `mise`; docs under `docs/`.

Check state first with `memoryd status` (config, store count, service, git hooks, resources).

## Critical facts (read before writing anything)

1. The daemon is the single zvec owner. CLI commands and MCP tools route store operations over `$AGENT_MEMORYD_HOME/agent-memoryd.sock`; if the daemon is not running, store operations fail.
2. `add` is an **upsert by `id`**. Pass a stable `id` for idempotency; **omitting `id` in a loop creates duplicates**.
3. **CLI `add` dash gotcha**: the body is a trailing positional arg. A body starting with `-` (markdown bullet) is parsed as a flag (`unknown shorthand flag`). Pass it after a `--` sentinel and use `--flag=value` form. MCP `add` is immune (body is a JSON field).
4. Direct `add` stores the body **verbatim**. Only daemon producers and `reflect` summarize.
5. `reindex` backfills embeddings for records that were written without an embedder or when embedding failed. It does not rebuild from a JSONL source.
6. **`uninstall --yes` deletes the entire data root** (including `zvec/`). Back up before destructive ops.
7. CLI output is pretty JSON on stdout — pipe through `jq`/`python3` to extract fields.

## Common tasks

Add / retrieve from the CLI (note `--` before the body):

```sh
memoryd add --id=note:setup --kind=fact --project=myproj \
  --summary="Preferred test command" -- "Run tests with: mise run test"
memoryd search --project myproj "test command"     # -> [{id,summary,score,...}]
memoryd get note:setup                             # full record
memoryd forget note:setup
```

From an agent, prefer the MCP tools: `search(query, project?, kind?, limit?)` → `get(id)` → `add(body, id?, kind?, project?, source?, summary?)` / `forget(id)` / `reflect(...)`.

Full flag/field details: [references/cli.md](references/cli.md) and [references/mcp.md](references/mcp.md).

## Retrieval pattern (progressive disclosure)

1. `search` for compact summaries + ids (keeps the turn small).
2. `get` only the ids that matter to pull full bodies.
3. `add`/`forget` to maintain memories during work — stable `id`, consistent `kind`.
4. `reflect` near the end of a meaningful session to capture durable preferences, instructions, decisions, and facts.

`kind` is free-form (common: `fact`, `note`, `feedback`, `preference`, `instruction`, `reflection`). `search` filters `kind` and `project` by exact match. Data model, kinds, id schemes, and store/index/concurrency semantics: [references/data-model.md](references/data-model.md).

## Bulk import (markdown/text/JSONL notes)

- Simplest: `memoryd init --import <path> --import-project <name>` (one `kind=note` memory per markdown/text file; JSONL preserves records).
- Fine control on a running install: `scripts/import_markdown_notes.sh <dir> --project <name> [--exclude <dir>]... [--dry-run]` — idempotent stable ids, per-file control, handles the dash gotcha. Tested; `--dry-run` first.

Caveats (built-in importer only skips `.git/node_modules/vendor/.cache/.DS_Store`; one project per batch) and verification steps: [references/bulk-import.md](references/bulk-import.md).

## Daemon, ingestion, config

The daemon polls `transcript_roots` for idle `.jsonl` transcripts and processes queued git events, sending source material (never stored raw) plus existing summaries to `summarizer_command`, storing distilled memories with `source` pointers. macOS `init` runs it via launchd. Git hooks only enqueue events (fast); `reflect` reuses the same summarizer path on demand.

Config schema, ingestion details, summarizer contract, launchd/logs, git-hook behavior, and privacy notes: [references/daemon.md](references/daemon.md).

## Developing the codebase

```sh
mise install
mise run test            # go test ./...
mise run zvec-libs       # install/update native zvec libraries
mise run build           # -> ./memoryd (zvec-backed, cgo)
mise run install-local   # atomic replace of ~/.local/bin/memoryd
```

The **installed binary can lag the source**. If a documented flag is missing (e.g. `init --import`), compare `memoryd --version` vs `./memoryd --version`, then `mise run build && mise run install-local`. Key source: `internal/memory` (store/record/search), `internal/app` (CLI + MCP), `internal/config`, `internal/daemon`, `internal/ingest`, `internal/importmem`, `internal/summarizer`. Repo `docs/` is the authoritative spec; keep it and this skill in sync when behavior changes.

## Reference map

- [references/cli.md](references/cli.md) — every subcommand, flags, output shapes.
- [references/mcp.md](references/mcp.md) — MCP tools, schemas, client config, retrieval pattern.
- [references/data-model.md](references/data-model.md) — record schema, kinds, ids, store/index/concurrency semantics, gotchas.
- [references/daemon.md](references/daemon.md) — daemon, transcript/git ingestion, summarizer, launchd, config schema, privacy.
- [references/bulk-import.md](references/bulk-import.md) — `init --import` vs the bundled script, the dash gotcha, verification.
- `scripts/import_markdown_notes.sh` — idempotent markdown/text importer for a running install.
