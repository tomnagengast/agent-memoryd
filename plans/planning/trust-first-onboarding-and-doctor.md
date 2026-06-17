# Trust-First Onboarding And Doctor

> updated: 2026-06-17

## Goal

Make first-run setup feel safe, observable, and reversible. Users should understand exactly what `agent-memoryd` will install, what local data it may read, and what command receives source material before they enable daemon ingestion or global Git hooks.

## Problem

The product currently demonstrates the most powerful path first. `init` creates config, may import memory, writes global Git hooks, and starts launchd by default on macOS.

Key code and docs:

- `README.md:14` recommends `./agent-memoryd init` in Quick Start.
- `README.md:20` notes global hooks and launchd installation.
- `docs/getting-started.md:17` says daemon polling starts immediately.
- `internal/app/app.go:87` defines `init` as the combined setup flow.
- `internal/app/app.go:118` installs managed Git hooks.
- `internal/app/app.go:127` starts launchd unless `--no-daemon` is passed.
- `internal/config/config.go:63` defaults transcript roots to Claude and Codex session directories.

This is efficient for an informed user, but it asks for high trust before the product has proven control and visibility.

## Product Direction

Position `agent-memoryd` as a trusted local memory layer first, and an automatic ingestion daemon second.

Default product posture:

- Manual/MCP-first setup.
- Explicit opt-in for background daemon.
- Explicit opt-in for global Git hooks.
- Explicit visibility into transcript roots and summarizer command.
- Reversible lifecycle with clear `uninstall` and data-root warnings.

## Proposed CLI Shape

Add a doctor command:

```sh
agent-memoryd doctor
agent-memoryd doctor --json
```

Add dry-run setup:

```sh
agent-memoryd init --dry-run
```

Add setup modes:

```sh
agent-memoryd init --mcp-only
agent-memoryd init --with-daemon
agent-memoryd init --with-git-hooks
agent-memoryd init --with-transcripts
```

Keep compatibility flags:

```sh
agent-memoryd init --fresh
agent-memoryd init --import <path>
agent-memoryd init --no-daemon
```

## Doctor Checks

`doctor` should report:

- Config path and whether it exists.
- Data root path and safety checks.
- Store path and memory count.
- Index backend and index health.
- Whether configured transcript roots exist.
- Whether `summarizer_command` is configured and executable.
- Whether `codex exec` is likely available for the default summarizer.
- Git hook status and whether global `core.hooksPath` is already set.
- launchd plist status on macOS.
- MCP command snippet.
- Any privacy-sensitive paths that daemon ingestion would scan.

## First-Run UX

Interactive `init` should present choices in plain language:

```text
How do you want to start?

1. MCP/manual only
   Creates local files and lets agents use search/get/add/forget. No daemon, no hooks.

2. Daemon without Git hooks
   Starts background transcript scanning. Shows transcript roots before enabling.

3. Full automatic mode
   Starts daemon and installs managed global Git hooks.
```

Before enabling daemon or hooks, print:

- Paths that will be read.
- Files that will be written.
- The summarizer command that receives raw source material.
- How to stop/uninstall.

## Documentation Changes

Update Quick Start to lead with MCP/manual mode:

```sh
./agent-memoryd init --mcp-only
./agent-memoryd doctor
./agent-memoryd status
```

Move daemon and Git hooks into an explicit “Enable Automatic Ingestion” section.

Add a privacy callout near the top of README and Getting Started, not only in daemon docs.

## Acceptance Criteria

- A user can initialize MCP/manual mode without installing launchd or changing global Git config.
- `init --dry-run` prints planned resources and exits without writing files or changing git/launchd state.
- `doctor` reports actionable warnings for missing summarizer, missing transcript roots, stale index, and hook conflicts.
- README Quick Start no longer defaults to high-trust automatic ingestion.

## Tests

- `init --mcp-only` creates config/store/manifest but does not install hooks or launchd.
- `init --dry-run` does not create data root resources.
- `doctor --json` produces stable parseable output.
- Existing `init --no-daemon`, `--fresh`, and `--import` behavior remains covered.

## Open Questions

- Should `--mcp-only` become the default behavior for non-interactive `init`?
- Should the old full-init behavior move behind `--automatic` or remain default for compatibility?
- Should `doctor` attempt a real summarizer smoke test, or only validate executability?
