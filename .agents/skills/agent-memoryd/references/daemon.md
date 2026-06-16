# Daemon, ingestion, and config

Table of contents
- Config schema and location
- The daemon
- Transcript ingestion
- Git ingestion
- Summarizer command
- launchd and logs
- Privacy

## Config schema and location

Config is JSON at `$AGENT_MEMORYD_HOME/config.json`; when `AGENT_MEMORYD_HOME` is unset the root defaults to `~/.local/share/agent-memoryd`. `init` writes defaults. Missing keys fall back to built-in defaults; `~/` and `$ENV` are expanded on load.

| key | default | meaning |
|---|---|---|
| `root` | `~/.local/share/agent-memoryd` | managed data dir; **`uninstall --yes` deletes it** |
| `store_path` | `<root>/memories.jsonl` | JSONL source of truth |
| `index_backend` | `lexical` | `lexical` or `zvec` (needs zvec build) |
| `zvec_path` | `<root>/zvec` | zvec index dir (zvec backend only) |
| `spool_dir` | `<root>/spool` | queued git events |
| `transcript_roots` | `~/.claude/projects`, `~/.codex/sessions` | dirs scanned for idle `.jsonl` transcripts |
| `summarizer_command` | `codex exec --sandbox read-only --skip-git-repo-check --ephemeral -` | distills source material into memories |
| `summarizer_timeout` | `5m0s` | bound per summarizer run |
| `memory_context_limit` | `12` | existing summaries sent to summarizer for dedup |
| `poll_interval` | `10s` | daemon ingest cadence |
| `idle_after` | `2m0s` | transcript must be unchanged this long before indexing |

`status` prints the live config and the `resources.json` manifest (each managed path with an `exists` flag).

## The daemon

`agent-memoryd daemon` is the resident worker; `scan-once` runs a single pass. It processes queued git events and scans `transcript_roots` every `poll_interval`. On macOS `init` runs it via launchd by default. Producers never store raw source material — they send it to the summarizer and store only the distilled result with a `source` pointer.

## Transcript ingestion

- Scans each `transcript_roots` entry for `.jsonl` files.
- A transcript must be unchanged for `idle_after` before indexing.
- Files older than the `resources.json` manifest creation time are skipped, so a fresh `init` does not backfill old history.
- Eligible transcripts + existing memory summaries go to `summarizer_command`; stored memories include the transcript path in `source` and a `More detail: Transcript: ...` body reference.

The MCP `reflect` tool runs this same path on demand (session text, an explicit transcript, or the newest one).

## Git ingestion

Git hooks do **not** summarize inline; they enqueue an event and return fast:

```sh
agent-memoryd enqueue-git --repo "$(git rev-parse --show-toplevel)" --sha "$(git rev-parse HEAD)"
```

The daemon later runs `git show --stat`, sends that plus existing summaries to the summarizer, and stores distilled memories with `repo@sha` in `source`. The event file is deleted after success.

Managed hooks (`post-commit`, `post-merge`, `post-rewrite`) live under `<root>/git-hooks`. `init` sets global `core.hooksPath` to that dir **only if** no global hook path is already configured; existing global hooks are left untouched. The managed hooks first run any same-named repo-local hook, then enqueue. They need `agent-memoryd` on `PATH`.

## Summarizer command

Replace `summarizer_command` in `config.json` with any local agent command that reads a prompt on stdin and returns JSON shaped like:

```json
{"memories":[{"kind":"preference","summary":"short summary","body":"concise durable memory"}]}
```

On failure the daemon logs the command failure and output byte counts, but **redacts** subprocess stdout/stderr because they may contain raw transcript or git material.

## launchd and logs

`init` writes `~/Library/LaunchAgents/dev.agent-memoryd.plist`, then `launchctl bootstrap` + `kickstart`. The plist captures `AGENT_MEMORYD_HOME` and the installing shell's `PATH` so `codex` (or your summarizer) resolves under launchd. Logs go to `<root>/logs/agent-memoryd.{out,err}.log`. Render without installing: `agent-memoryd launchd-plist --bin /abs/path/to/agent-memoryd`.

## Privacy

Ingestion is local, but the summarizer receives raw transcript/git material on stdin. Review `summarizer_command`, `transcript_roots`, and git-hook installation before running the resident daemon on sensitive projects. To disable transcript ingestion, narrow or empty `transcript_roots`.
