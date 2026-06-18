# Bulk importing notes

Two ways to load an existing tree of markdown/text/JSONL notes into the store. Pick based on whether you need fine control.

## Option A — built-in `init --import` (simplest)

```sh
memoryd init --import ~/notes/agent --import-project notes
```

Handled by `internal/importmem`:
- Directory walk imports `.jsonl`, `.md`/`.markdown`, `.txt` files.
- Markdown/text → one memory per file, `kind=note`, body verbatim, summary from the first heading/non-blank line (140 chars), `source` = path, id `import:<sha1(path)>`.
- JSONL → one memory per line, preserving each record's own `kind`/`project`/`source`/timestamps; `--import-project` only fills blank projects.
- Skips dirs named `.git`, `node_modules`, `vendor`, `.cache`, `.DS_Store`.

Limits:
- Runs as part of `init` (intended for setup), not as a standalone command on a running install.
- Only skips the fixed dir list above — it does **not** skip arbitrary junk dirs (e.g. a `git-repair/` backup tree gets imported).
- One project for the whole markdown/text batch; no per-file project.
- Requires a binary new enough to have the flag (`init --help` must show `--import`). Rebuild if missing: `mise run build && mise run install-local`.

## Option B — bundled script (fine control, any time)

`scripts/import_markdown_notes.sh` runs against an already-initialized store and adds per-file control:

```sh
scripts/import_markdown_notes.sh ~/notes/agent --project notes --exclude git-repair
scripts/import_markdown_notes.sh ./docs --project myproj --dry-run
```

Flags: `--project NAME`, `--id-prefix PFX` (default `note`), `--exclude DIR` (repeatable), `--dry-run`. Honors `AGENT_MEMORYD_BIN` to point at a specific binary.

Behavior: imports `.md`/`.markdown`/`.txt`, one `kind=note` memory per file, body verbatim, stable id `<prefix>:<relpath-slug>` (idempotent re-runs upsert), summary from first heading/non-blank line, `source` = absolute path. Skips empty files and any `--exclude` dirs. After a real run it suggests `memoryd reindex`.

For per-file project mapping (e.g. `maple*` → `maple`, applications → `job-search`), edit the `--project` assignment into a `case "$stem" in ... esac` block — straightforward to extend.

## The critical gotcha (CLI only)

`memoryd add` takes the body as a trailing positional arg. Markdown files frequently start with `- ` (a bullet), so cobra parses the body as a flag and fails:

```
unknown shorthand flag: ' ' in - 2026-06-06 ...
```

Fix, applied by the script: pass the body after a `--` sentinel and use `--flag=value` form.

```sh
memoryd add --id=note:x --kind=note --project=p --source=f --summary="t" -- "$(cat f)"
```

MCP `add` passes the body as a JSON field and is immune — prefer it when scripting from an agent.

## Verify an import

```sh
# total records and store health
memoryd status
# spot-check retrieval
memoryd search --kind note "some topic"
```

To undo a batch, `forget` each id (stable-id schemes make the id set predictable), then `reindex`.
