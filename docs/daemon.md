# Daemon

`agent-memoryd daemon` runs the resident ingest worker. It processes queued git events and scans configured transcript roots on a polling interval.

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

A transcript must be unchanged for `idle_after` before it is indexed. The daemon extracts a compact session memory with the transcript path, working directory, modified time, assistant turn count, tool call count, first user prompt, and last user prompt.

Transcript memories use kind `session`.

## Git Event Ingestion

Git hooks should not summarize commits inline. They enqueue a small event file with the repository path and commit sha:

```sh
./agent-memoryd enqueue-git \
  --repo "$(git rev-parse --show-toplevel)" \
  --sha "$(git rev-parse HEAD)"
```

The daemon later reads the event, runs `git show --stat`, stores a `git-summary` memory, and removes the event file.

## launchd

Render a macOS LaunchAgent plist:

```sh
./agent-memoryd launchd-plist --bin /absolute/path/to/agent-memoryd
```

The command writes plist XML to stdout. It does not install or load the service. If you install it at the standard managed path, `~/Library/LaunchAgents/dev.agent-memoryd.plist`, then `uninstall --yes` will try to boot it out and remove it.

## Logs

The rendered LaunchAgent writes stdout and stderr logs under:

```text
$AGENT_MEMORYD_HOME/logs
```

## Privacy

Transcript ingestion is local, but it may store prompts and transcript metadata from the configured roots. Review `transcript_roots` before running a resident daemon on directories that contain sensitive sessions.
