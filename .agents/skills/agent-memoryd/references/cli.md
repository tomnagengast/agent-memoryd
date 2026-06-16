# CLI reference

All commands print pretty JSON to stdout. The binary is `agent-memoryd` (installed to `~/.local/bin/agent-memoryd`). Inside the repo, the freshly built binary is `./agent-memoryd`.

Table of contents
- Memory commands: add, search, get, forget, reindex
- Lifecycle: init, status, uninstall
- Daemon/ingest: daemon, scan-once, enqueue-git
- Service: launchd-plist, mcp
- Global: --version, completion

## add — create or update a memory

```
agent-memoryd add [flags] <body>
  --id string        stable id for upsert (omit -> random id every call)
  --kind string      memory kind (default "fact")
  --project string   project scope
  --source string    source reference
  --summary string   summary (omit -> derived from body, 180 chars)
```

Body is the single positional arg, stored verbatim. **Pass it after `--` and use `--flag=value` form** so bodies starting with `-` are not parsed as flags:

```sh
agent-memoryd add --id=note:setup --kind=note --project=myproj \
  --source=/path/file.md --summary="Setup steps" -- "$(cat /path/file.md)"
```

Returns the stored record (including generated id and timestamps).

## search — find memories

```
agent-memoryd search [flags] <query>
  --kind string      exact-match kind filter
  --project string   exact-match project filter
  --limit int        max results (default 5, capped at 50)
```

Returns `[{id, kind, project, source, summary, score}]` ordered by score. Scores fraction of query tokens matched in `summary+body+kind+project`. Empty query errors. Use this first; expand with `get`.

```sh
agent-memoryd search "workos setup"
agent-memoryd search --project myproj --limit 10 "deploy"
```

## get — fetch one full memory

```
agent-memoryd get <id>
```

Returns the full record, or `{"found": false, "id": "..."}` if absent.

## forget — delete one memory

```
agent-memoryd forget <id>
```

Returns `{"ok": true, "id": "..."}`, or `{"ok": false, ...}` if not found. Removes from store and index.

## reindex — rebuild the retrieval index

```
agent-memoryd reindex
```

Rebuilds the configured index from `memories.jsonl`. Run after hand-editing the store or any out-of-band change. Returns `{"ok": true}`.

## init — set up the managed install

```
agent-memoryd init [flags]
  --path string            config path (default <root>/config.json)
  --no-daemon              do not install/start the launchd daemon
  --fresh                  start empty, no import prompt
  --import string          import JSONL file or markdown/text file/dir
  --import-project string  project for imported markdown/text records
```

Creates the data root, `config.json`, `memories.jsonl`, git spool, managed global git hooks, logs dir, and `resources.json`. On macOS it also installs and starts the LaunchAgent (unless `--no-daemon`). Interactive runs prompt fresh-vs-import; scripts use `--fresh`/`--import`. `--fresh` and `--import` are mutually exclusive. See [bulk-import.md](bulk-import.md).

Note: `--fresh`/`--import`/`--import-project` are recent source flags. If the installed binary lacks them (`init --help` shows only `--no-daemon`/`--path`), rebuild: `mise run build && mise run install-local`.

## status — report config and managed resources

```
agent-memoryd status
```

Prints `initialized`, system/MCP help, loaded `config`, `store` status (path, index, memory count), launchd service status, git hook status, and every manifest resource with an `exists` flag.

## uninstall — remove managed resources

```
agent-memoryd uninstall --yes
```

Without `--yes` it prints what would be removed. With `--yes` it removes the managed `root` directory and the LaunchAgent plist, and unsets global `core.hooksPath` if it points at the managed hooks. **Deletes `memories.jsonl`** — back up first.

## daemon / scan-once — ingest worker

```
agent-memoryd daemon       # run resident worker (foreground)
agent-memoryd scan-once    # one ingest pass, then exit
```

See [daemon.md](daemon.md). On macOS `init` runs the daemon via launchd; run `daemon` manually only for debugging.

## enqueue-git — queue a commit for summarization

```
agent-memoryd enqueue-git --repo <path> --sha <sha>   # sha default HEAD
```

Writes a small event to the spool for the daemon to summarize later. Used by the managed git hooks.

## launchd-plist — render the LaunchAgent plist

```
agent-memoryd launchd-plist [--bin <path>] [--label dev.agent-memoryd]
```

Writes plist XML to stdout for inspection or manual install; does not install.

## mcp — run the stdio MCP server

```
agent-memoryd mcp
```

Speaks MCP over stdio. Configure an MCP client to launch it. See [mcp.md](mcp.md).

## Global

```
agent-memoryd --version    # build metadata (version, commit, date)
agent-memoryd completion <shell>
```
