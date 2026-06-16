# Daemon

`agent-memoryd daemon` runs the resident ingest worker. It processes queued git events and scans configured transcript roots on a polling interval.

On macOS, `agent-memoryd init` installs and starts this daemon through launchd by default. Use `agent-memoryd init --no-daemon` to skip service setup.

Run the daemon in the foreground:

```sh
./agent-memoryd daemon
```

Run one pass without staying resident:

```sh
./agent-memoryd scan-once
```

## Transcript Ingestion

The daemon scans each configured `transcript_roots` entry for `.jsonl` files. By default those roots are:

```text
~/.claude/projects
~/.codex/sessions
```

A transcript must be unchanged for `idle_after` before it is indexed. The daemon reads the transcript and passes it, transcript metadata, and existing memory summaries to the configured `summarizer_command`. The raw transcript is not stored as the memory body.

The summarizer returns zero or more distilled memories. Those memories should capture durable information learned during the session, such as user preferences, standing instructions, project decisions, or follow-up context. Stored memories include the transcript path in `source` and a `More detail: Transcript: ...` reference in the body for progressive disclosure.

The MCP `reflect` tool uses the same summarizer behavior on demand. It can summarize session text supplied by the MCP client, an explicit transcript path, or the newest configured transcript.

## Git Event Ingestion

Git hooks should not summarize commits inline. They enqueue a small event file with the repository path and commit sha:

```sh
./agent-memoryd enqueue-git \
  --repo "$(git rev-parse --show-toplevel)" \
  --sha "$(git rev-parse HEAD)"
```

The daemon later reads the event, runs `git show --stat`, and passes that git summary plus existing memory summaries to the same configured summarizer. The raw git output is not stored as the memory body.

The summarizer returns zero or more distilled memories. Stored git memories include `repo@sha` in `source` and a `More detail:` reference with the commit and local repository path for progressive disclosure. The event file is removed after the event is successfully processed.

## Summarizer Command

The default summarizer command is:

```json
["codex", "exec", "--sandbox", "read-only", "--skip-git-repo-check", "--ephemeral", "-"]
```

You can replace `summarizer_command` in `config.json` with another local agent command. It must read the prompt from stdin and return JSON with a top-level `memories` array.

## launchd

`init` writes the managed LaunchAgent to:

```text
~/Library/LaunchAgents/dev.agent-memoryd.plist
```

It then runs `launchctl bootstrap` and `launchctl kickstart` so the daemon is up immediately.

The plist includes `AGENT_MEMORYD_HOME` and the PATH from the shell that ran `init`, so default commands such as `codex` can be found when launchd starts the daemon.

Render a macOS LaunchAgent plist without installing it:

```sh
./agent-memoryd launchd-plist --bin /absolute/path/to/agent-memoryd
```

The command writes plist XML to stdout for inspection or advanced manual installs.

## Logs

The rendered LaunchAgent writes stdout and stderr logs under:

```text
$AGENT_MEMORYD_HOME/logs
```

## Privacy

Transcript and git ingestion are local, but the configured summarizer receives raw source material on stdin. Review `summarizer_command`, `transcript_roots`, and git hook installation before running a resident daemon on sensitive projects.
