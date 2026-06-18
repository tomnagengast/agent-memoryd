# Config

`agent-memoryd` reads JSON config from:

```text
$AGENT_MEMORYD_HOME/config.json
```

When `AGENT_MEMORYD_HOME` is unset, the root defaults to:

```text
~/.local/share/agent-memoryd
```

`agent-memoryd init` writes a default config and a `resources.json` manifest in the same root.

## Example

```json
{
  "root": "/Users/you/.local/share/agent-memoryd",
  "zvec_path": "/Users/you/.local/share/agent-memoryd/zvec",
  "spool_dir": "/Users/you/.local/share/agent-memoryd/spool",
  "transcript_roots": [
    "/Users/you/.claude/projects",
    "/Users/you/.codex/sessions"
  ],
  "summarizer_command": [
    "codex",
    "exec",
    "--sandbox",
    "read-only",
    "--skip-git-repo-check",
    "--ephemeral",
    "-"
  ],
  "summarizer_timeout": "5m0s",
  "embedder_command": [],
  "embedder_timeout": "30s",
  "embedding_dim": 768,
  "search_fts_weight": 0.5,
  "search_vector_weight": 0.5,
  "lock_timeout": "5s",
  "memory_context_limit": 12,
  "poll_interval": "10s",
  "idle_after": "2m0s"
}
```

## Reference

`root` is the managed data directory. `uninstall --yes` removes this directory, so keep it dedicated to `agent-memoryd`.

`store_path` is a legacy field retained only for first-run migration. On first open, if `memories.jsonl` exists in the data root, it is imported once into the zvec store and renamed `memories.jsonl.migrated`. This field has no effect after migration.

`ingest-state.json` is managed operational state under `root`. It records transcript and git event fingerprints that were processed, failed, or quarantined so the daemon does not retry the same unchanged source every poll.

`zvec_path` is the on-disk zvec store directory. This is the durable store for all memories.

`spool_dir` holds queued git events. The managed global Git hooks write small JSON files here, and the daemon passes each event's `git show` output to the summarizer.

`transcript_roots` lists directories to scan for idle `.jsonl` agent transcripts. The defaults cover Claude project transcripts and Codex sessions. The daemon only ingests transcript files modified after `init` wrote the resource manifest. Remove or narrow these paths if you do not want transcript ingestion.

`summarizer_command` is the external command used by daemon producers to distill transcripts and git summaries into durable memories. The command receives a prompt on stdin and must return JSON shaped like `{"memories":[{"kind":"preference","summary":"short summary","body":"concise durable memory"}]}`. The default command uses `codex exec` in read-only ephemeral mode. Set this to another command if you want a different local summarization agent.

`summarizer_timeout` bounds one summarizer run.

`embedder_command` is the external command used to embed memory text as a vector for semantic search. The command receives the text on stdin and must return a JSON array of float32 values. When empty or omitted, embedding is disabled and only full-text search is used. Example: `["my-embedder", "--dim", "768"]`.

`embedder_timeout` bounds one embedding call. Default is `30s`.

`embedding_dim` is the expected dimension of vectors returned by `embedder_command`. Default is `768`. This must match the model used by your embedder. Changing this after the store is created requires a fresh store.

`search_fts_weight` is the blend weight applied to full-text search results when both FTS and vector legs return results. Default is `0.5`. Increase this to favor keyword matching.

`search_vector_weight` is the blend weight applied to vector search results. Default is `0.5`. Increase this to favor semantic similarity. Has no effect when `embedder_command` is not configured.

`lock_timeout` is how long `init` waits for the daemon socket to become available after starting the managed service. Default is `5s`.

`memory_context_limit` controls how many existing memory summaries are passed to the summarizer so it can avoid duplicating old memories and identify genuinely new facts, preferences, instructions, or decisions.

`poll_interval` controls how often the daemon runs an ingest pass.

`idle_after` controls how long a transcript must be unchanged before it is indexed.

## Import

`init` can start fresh or import existing memories after the daemon starts. In an interactive terminal it prompts for that choice. In scripts, use `--fresh` or `--import <path>`. `--import` requires the daemon and cannot be combined with `--no-daemon`.

`--import` accepts an agent-memoryd JSONL file, a markdown file, a text file, or a directory containing markdown/text files. Use `--import-project <name>` to assign a project to imported markdown and text records. JSONL imports preserve each record's existing project.

## Resource Manifest

`init` writes `resources.json` so later lifecycle commands can show and remove the resources `agent-memoryd` owns. `status` reports each resource with an `exists` flag. The manifest includes the ingest state file, managed global Git hooks directory, and hook files.
