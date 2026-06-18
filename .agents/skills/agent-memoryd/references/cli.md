# CLI reference

All commands print pretty JSON to stdout. The binary is `memoryd` (installed to `~/.local/bin/memoryd`). Inside the repo, the freshly built binary is `./memoryd`.

Table of contents
- Memory commands: add, search, get, forget, reindex
- Lifecycle: init, status, uninstall
- Daemon/ingest: daemon, scan-once, enqueue-git
- Service: launchd-plist, mcp
- Global: --version, completion

## add — create or update a memory

```
memoryd add [flags] <body>
  --id string        stable id for upsert (omit -> random id every call)
  --kind string      memory kind (default "fact")
  --project string   project scope
  --source string    source reference
  --summary string   summary (omit -> derived from body, 180 chars)
```

Body is the single positional arg, stored verbatim. **Pass it after `--` and use `--flag=value` form** so bodies starting with `-` are not parsed as flags:

```sh
memoryd add --id=note:setup --kind=note --project=myproj \
  --source=/path/file.md --summary="Setup steps" -- "$(cat /path/file.md)"
```

Returns the stored record (including generated id and timestamps).

## search — find memories

```
memoryd search [flags] <query>
  --kind string      exact-match kind filter
  --project string   exact-match project filter
  --limit int        max results (default 5, capped at 50)
```

Returns `[{id, kind, project, source, summary, score}]` ordered by score. Scores fraction of query tokens matched in `summary+body+kind+project`. Empty query errors. Use this first; expand with `get`.

```sh
memoryd search "workos setup"
memoryd search --project myproj --limit 10 "deploy"
```

## get — fetch one full memory

```
memoryd get <id>
```

Returns the full record, or `{"found": false, "id": "..."}` if absent.

## forget — delete one memory

```
memoryd forget <id>
```

Returns `{"ok": true, "id": "..."}`, or `{"ok": false, ...}` if not found. Removes from store and index.

## reindex — rebuild the retrieval index

```
memoryd reindex
```

Backfills vector embeddings for records that were stored without one. Returns `{"ok": true}`.

## init — set up the managed install

```
memoryd init [flags]
  --path string            config path (default <root>/config.json)
  --no-daemon              do not install/start the launchd daemon
  --fresh                  start empty, no import prompt
  --import string          import JSONL file or markdown/text file/dir
  --import-project string  project for imported markdown/text records
```

Creates the data root, `config.json`, zvec store, git spool, managed global git hooks, logs dir, and `resources.json`. On macOS it also installs and starts the LaunchAgent unless skipped. Interactive runs use a guided onboarding flow for fresh-vs-import setup, default transcript ingestion roots, and daemon startup; scripts use `--fresh`/`--import`/`--no-daemon`. `--fresh` and `--import` are mutually exclusive. See [bulk-import.md](bulk-import.md).

Note: `--fresh`/`--import`/`--import-project` are recent source flags. If the installed binary lacks them (`init --help` shows only `--no-daemon`/`--path`), rebuild: `mise run build && mise run install-local`.

## status — report config and managed resources

```
memoryd status
```

Prints `initialized`, system/MCP help, loaded `config`, `store` status (path, index, memory count), launchd service status, git hook status, and every manifest resource with an `exists` flag.

## uninstall — remove managed resources

```
memoryd uninstall --yes
```

Without `--yes` it prints what would be removed. With `--yes` it removes the managed `root` directory and the LaunchAgent plist, and unsets global `core.hooksPath` if it points at the managed hooks. This deletes the zvec store, so back up first.

## daemon / scan-once — ingest worker

```
memoryd daemon       # run resident worker (foreground)
memoryd scan-once    # one ingest pass, then exit
```

See [daemon.md](daemon.md). On macOS `init` runs the daemon via launchd; run `daemon` manually only for debugging.

## enqueue-git — queue a commit for summarization

```
memoryd enqueue-git --repo <path> --sha <sha>   # sha default HEAD
```

Writes a small event to the spool for the daemon to summarize later. Used by the managed git hooks.

## launchd-plist — render the LaunchAgent plist

```
memoryd launchd-plist [--bin <path>] [--label dev.memoryd]
```

Writes plist XML to stdout for inspection or manual install; does not install.

## mcp — run the stdio MCP server

```
memoryd mcp
```

Speaks MCP over stdio. Configure an MCP client to launch it. See [mcp.md](mcp.md).

## Global

```
memoryd --version    # build metadata (version, commit, date)
memoryd completion <shell>
```
