# Daemon

`memoryd daemon` runs the resident ingest worker. It processes queued git events and scans configured transcript roots on a polling interval.

On macOS, `memoryd init` installs and starts this daemon through launchd by default. Use `memoryd init --no-daemon` to skip service setup.

Run the daemon in the foreground:

```sh
./memoryd daemon
```

Run one pass without staying resident:

```sh
./memoryd scan-once
```

## Store Ownership

The daemon holds the zvec collection exclusively for its lifetime. It serves all store operations (add, search, get, forget, list, status, reindex) over a Unix socket at `$AGENT_MEMORYD_HOME/agent-memoryd.sock`. CLI commands and the MCP server route through that socket and never open zvec directly.

## Transcript Ingestion

The daemon scans each configured `transcript_roots` entry for `.jsonl` files and exported OpenCode `.json` sessions. OpenCode data roots are handled through `opencode session list` plus `opencode export <sessionID>`, avoiding a SQLite dependency. By default those roots are:

```text
~/.claude/projects
~/.codex/sessions
~/.local/share/opencode
```

A transcript must be unchanged for `idle_after` before it is indexed. The daemon also skips transcript files whose modification time is older than the `resources.json` manifest creation time, so a fresh `init` does not backfill old agent history. The daemon reads eligible transcripts and passes transcript metadata, raw transcript content, and existing memory summaries to the configured `summarizer_command`. The raw transcript is not stored as the memory body.

The summarizer returns zero or more distilled memories. Those memories should capture durable information learned during the session, such as user preferences, standing instructions, project decisions, or follow-up context. Stored memories include the transcript path in `source` and a `More detail: Transcript: ...` reference in the body for progressive disclosure.

Processed transcript fingerprints are recorded in `ingest-state.json`, so unchanged transcripts are not summarized again on every daemon poll. If summarization fails, the daemon records the failure, backs off retries, and quarantines that transcript fingerprint after repeated failures. A later transcript modification changes the fingerprint and makes it eligible again.

The MCP `reflect` tool uses the same summarizer behavior on demand. It can summarize session text supplied by the MCP client, an explicit transcript path, or the newest configured transcript.

## Git Event Ingestion

Git hooks should not summarize commits inline. They enqueue a small event file with the repository path and commit sha:

```sh
./memoryd enqueue-git \
  --repo "$(git rev-parse --show-toplevel)" \
  --sha "$(git rev-parse HEAD)"
```

The daemon later reads the event, runs `git show --stat`, and passes that git summary plus existing memory summaries to the same configured summarizer. The raw git output is not stored as the memory body.

The summarizer returns zero or more distilled memories. Stored git memories include `repo@sha` in `source` and a `More detail:` reference with the commit and local repository path for progressive disclosure. The event file is removed after the event is successfully processed.

Git event failures are tracked in `ingest-state.json` with the same backoff behavior. After repeated failures, the event is moved to `spool/failed/` so the top-level spool does not block future events.

## Summarizer Command

The default summarizer command is:

```json
[
  "codex",
  "exec",
  "--sandbox",
  "read-only",
  "--skip-git-repo-check",
  "--ephemeral",
  "-"
]
```

You can replace `summarizer_command` in `config.json` with another local agent command. It must read the prompt from stdin and return JSON with a top-level `memories` array.

If the summarizer command fails, daemon logs include the command failure and output byte counts, but subprocess stdout and stderr are redacted because they may contain raw transcript or git source material.

## launchd

`init` writes the managed LaunchAgent to:

```text
~/Library/LaunchAgents/dev.agent-memoryd.plist
```

It then runs `launchctl bootstrap` and `launchctl kickstart` so the daemon is up immediately.

The plist includes `AGENT_MEMORYD_HOME` and the PATH from the shell that ran `init`, so default commands such as `codex` can be found when launchd starts the daemon.

Render a macOS LaunchAgent plist without installing it:

```sh
./memoryd launchd-plist --bin /absolute/path/to/memoryd
```

The command writes plist XML to stdout for inspection or advanced manual installs.

## Logs

The rendered LaunchAgent writes stdout and stderr logs under:

```text
$AGENT_MEMORYD_HOME/logs
```

## Privacy

Transcript and git ingestion are local, but the configured summarizer receives raw source material on stdin. Review `summarizer_command`, `transcript_roots`, and git hook installation before running a resident daemon on sensitive projects.
