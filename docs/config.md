# Config

`agent-memoryd` reads JSON config from:

```text
$AGENT_MEMORYD_HOME/config.json
```

When `AGENT_MEMORYD_HOME` is unset, the root defaults to:

```text
~/.local/share/agent-memoryd
```

`agent-memoryd init` writes a default config and a `resources.json` manifest in
the same root.

## Example

```json
{
  "root": "/Users/you/.local/share/agent-memoryd",
  "store_path": "/Users/you/.local/share/agent-memoryd/memories.jsonl",
  "index_backend": "lexical",
  "zvec_path": "/Users/you/.local/share/agent-memoryd/zvec",
  "spool_dir": "/Users/you/.local/share/agent-memoryd/spool",
  "transcript_roots": [
    "/Users/you/.claude/projects",
    "/Users/you/.codex/sessions"
  ],
  "poll_interval": "10s",
  "idle_after": "2m0s"
}
```

## Reference

`root` is the managed data directory. `uninstall --yes` removes this directory,
so keep it dedicated to `agent-memoryd`.

`store_path` is the JSONL source store. It is the rebuildable source of truth
for memories.

`index_backend` selects the retrieval index. Use `lexical` for the default pure
Go build or `zvec` for a binary built with `mise run build-zvec`.

`zvec_path` is the on-disk zvec index directory.

`spool_dir` holds queued git events. Git hooks write small JSON files here, and
the daemon converts them into `git-summary` memories.

`transcript_roots` lists directories to scan for idle `.jsonl` agent
transcripts. The defaults cover Claude project transcripts and Codex sessions.
Remove or narrow these paths if you do not want transcript ingestion.

`poll_interval` controls how often the daemon runs an ingest pass.

`idle_after` controls how long a transcript must be unchanged before it is
indexed.

## Resource Manifest

`init` writes `resources.json` so later lifecycle commands can show and remove
the resources `agent-memoryd` owns. `status` reports each resource with an
`exists` flag.
