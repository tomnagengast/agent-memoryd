# Agent Memory Daemon

> updated: 2026-06-16T13:56:19-07:00

Research / design doc. Captures the design conversation for a local memory daemon for coding agents, built on zvec. Not yet a plan. Status: pre-plan, architecture agreed, a few forks open.

## Goal

A local, always-on service that captures what coding agents do (session transcripts + git history), stores it as structured, semantically searchable memory in zvec, and serves it back to any agent through pull-based MCP retrieval with progressive disclosure. Zero per-project setup for ingestion. Configured once globally for retrieval.

Working names: daemon `agent-memoryd`, CLI `agent-memory`.

## Positioning

"Local memory daemon for coding agents." Explicitly NOT a qmd replacement. qmd is document search over markdown. This is event capture + structured memory + git-history summarization + pull-based MCP retrieval. Different layer. State the distinction in the README to keep contributors from arguing the wrong comparison.

zvec sits a layer below qmd (closer to sqlite-vec). zvec does have BM25/FTS, sparse embeddings, and rerankers (RRF, cross-encoder), so the retrieval surface can be a genuine hybrid+rerank peer, not a semantic-only downgrade. The work is orchestrating the stages; the primitives exist.

## Prior art (NOT migration scope)

Tom's existing memory infra is prior art only. The public product owns its own store and does not import these in v1. This also resolves the source-of-truth debate: a fresh store never has to answer zvec-vs-markdown.

Two existing systems, both pure markdown, no semantic layer:

1. Hook-driven agent notes (hand-built). `~/notes/agent/` topical notes + `~/notes/agent/memory/YYYY-MM-DD.md` daily journal. Write: `session-end-memory.sh` (SessionEnd hook) spawns a detached headless `claude -p` summarizer off the main thread; `pre-compact-memory.sh` injects a main-thread flush prompt; `checkpoint` skill writes topical notes. Read: `session-start-context.sh` surfaces paths; `memory-check` skill. Drift: `~/notes/agent/memory/MEMORY.md` doesn't exist though hooks reference it; `memory-check` references nonexistent `learnings/LEARNINGS.md` + `ERRORS.md` and a qmd `notes` collection that has zero collections indexed.
2. Native per-fact memory (Claude Code feature, curated). `~/.claude/projects/<slug>/memory/` one fact per file (frontmatter: name, description, metadata.type ∈ user|feedback|project|reference) + `MEMORY.md` index with `[[wikilink]]` cross-refs. Heavy for maple (23 facts), light elsewhere, empty for explore-zvec.

Codex prior art worth reusing as pattern: `after-agent-knowledge.py` already implements out-of-band capture (notify `agent-turn-complete` -> detached background worker -> parse rollout JSONL -> `codex -p` summary -> daily file, with per-thread lock + cursor). Currently dead: codex `notify` slot is taken by the Computer Use client; the dispatcher that would run it is commented out at a stale path.

## Core design principle

Nothing runs on the main agent thread. Capture is out-of-band by construction. The resident daemon is the out-of-band substrate. The competing instruction is `AGENTS.md`/`CLAUDE.md:873` ("take notes in ~/notes/agent...") which forces inline main-thread writes; in the daemon world that instruction flips to read-only ("you may consult; a service captures memory after the turn").

## Architecture

Single resident LaunchAgent (per-user, not a system LaunchDaemon: needs login-session env, `~/.claude`/`~/.codex` access, network, notifications). `RunAtLoad` + `KeepAlive`. Plist in `~/Library/LaunchAgents/`.

Why resident vs spawn-per-event:
- Keeps the embedding model warm. Cold load is the expensive part (hundreds of MB). Amortized across every ingest.
- Daemon is the single writer to zvec. Sidesteps multi-process lock juggling (zvec is single-writer). This is a real win.

Components (map to `src/agent_memory/` submodules):
- `daemon/` lifecycle, queue draining, FSEvents watch loop, scheduling.
- `ingest/` transcript parsing + session summarization; git-range summarization.
- `store/` record model + vector store. MUST be an interface with a zvec adapter behind it (zvec is pre-1.0; do not leak its API upward; the documented import JSONL schema is the store-agnostic record contract).
- `retrieval/` hybrid search + rerank orchestration; progressive-disclosure tiers.
- `mcp/` HTTP/SSE MCP server exposing `search` and `get`.
- `cli/` status, config, manual search, install commands.

## Ingestion

Two sources, one worker.

Session capture (zero-setup): daemon watches configured transcript roots (`~/.claude/projects/*/`, `~/.codex/sessions/**/`) via FSEvents. Per-file byte cursor for incremental reads. New project / new harness = no setup; if it writes a transcript to a watched root it gets ingested.
- Caveat: filesystem watching loses the explicit "session ended" signal. Infer boundary via idle timeout (no writes for N min = done). This composes with timing the heavy summary for when no agent is competing for CPU. Idle-detection is the zero-setup baseline; keep lightweight harness hooks as an OPTIONAL precise end-of-session ping.
- Guard against ingesting the summarizer's own subprocess sessions (existing `CLAUDE_SUMMARIZER=1` pattern). Persist cursors + processed-session set across restarts.

Git-history summarization (event + scheduled):
- Global git hook via `git config --global core.hooksPath` so it applies to every repo with no per-repo install. Hook is enqueue-only: append `{repo, sha, ts}` to a spool dir/queue and return immediately. Robust to daemon-down (markers accumulate, drained on next start). Never blocks the commit.
- Relevant hooks: `post-commit`, `post-merge`, `post-rewrite`, `post-checkout`.
- Hooks can't see history that never passes through local git (teammate/CI/other-machine commits). Scheduled per-project sweep is the completeness backstop. So cadence is unified, not a fork: hook = event-driven incremental, sweep = backstop, both enqueue to the one worker.
- Per-project cursor `last_summarized_sha`; worker summarizes `last_summarized_sha..HEAD`.
- Layered summaries (progressive disclosure applied to history): per-commit one-liners -> topic/range summaries -> short project overview. Each tier a zvec record tagged project + range + tier level.

## Store + record schema

Single global collection, `project` scalar field, `filter="project = '...'"` (simpler capture than collection-per-project; cross-project recall stays possible).

Record (store-agnostic, also the documented import schema): `id`, `embedding`, `type` (session-memory | git-summary | fact), `project`, `tier`, `description`, `body`, `created`, `source_ref`.

## Retrieval + progressive disclosure

Two-tier read, pull-based by construction (MCP), so the agent expands only what it wants instead of us guessing what to inject.
- Index tier: semantic (or hybrid) search returns only `id`, `type`, `project`, `description`, score. Cheap. This is what an agent or SessionStart sees first. zvec: `output_fields=["description", ...]`.
- Detail tier: `get` by id returns full `body`. Called only for the few relevant hits. zvec: `fetch(id)`.

Delivery: daemon hosts an HTTP/SSE MCP endpoint on a fixed localhost port (a resident daemon can't be a 1:1 client-spawned stdio server). Register the URL once in each agent config (Claude mcpServers, codex `[mcp_servers.*]`). Fallback if a client misbehaves over HTTP: a trivial per-client stdio shim that proxies to the daemon, keeping warm model + zvec handle resident. Start HTTP/SSE; add shim only if needed. Thin CLI mirrors `search`/`get` for manual use.

## Privacy + security (deepest-designed area; a public launch is judged here)

Threat model: the store aggregates everything you've done across all projects into one queryable place. That aggregation is the value AND the danger. The MCP endpoint is an exfiltration surface.

- Transcript ingestion opt-in and boringly explicit. Default local-only. No default watch paths; require configured ones. Document exactly what is stored.
- Binding MCP to 127.0.0.1 is NOT access control. On a multi-process machine any local process / any agent handed the config can read your whole history. Add a bearer token or unix-socket-with-file-perms on the MCP surface.
- Transcripts carry secrets (printed env vars, tokens, dumped file contents). Opt-in is not sufficient: redact on ingest, treat the store as sensitive at rest.
- Provide pause, forget, export, reset. `forget` = hard delete from the vector store and any derived artifact, not a tombstone. `export` must be complete.

## Packaging + fresh-install story

CLI-driven, optimized for a clean machine:
1. `agent-memory init`
2. `agent-memory service install` (writes + loads the LaunchAgent)
3. `agent-memory hook install` (sets global `core.hooksPath` + drops the enqueue hook)
4. `agent-memory mcp install --client claude|codex` (patches + validates the config; do NOT make this a manual "paste this snippet" step — that's where fresh installs die)
5. `agent-memory status`

Hardest real risk (qmd's hard problem too): shipping an embedding runtime + model that works on a clean machine. zvec's local embedder wants Python 3.10–3.12; repo is otherwise 3.13. The daemon's own venv solves the version pin, but packaging a model + runtime for public users is nontrivial. qmd nailed this with bundled node-llama-cpp + auto-downloaded GGUF. Decide early because it shapes packaging:
- Option A: pin a private daemon Python + bundle/auto-download a local model (keeps the "boringly local" promise). Leaning here.
- Option B: default public build to an API embedder, local as opt-in.

v1 is macOS-only (launchd). Keep `launchd/` + service layer OS-specific so a systemd drop-in is a later addition, not a refactor.

## Scope: import, not migration

v1 explicitly does NOT support "import Tom's markdown memory system" (drags in private folder shapes, old hook assumptions, qmd experiments, harness-specific history; makes it feel like a personal migration tool). Generic import comes later:
- `agent-memory import markdown <dir>` for simple .md files.
- `agent-memory import jsonl <file>` for records matching the documented schema.
- `examples/importers/` where personal adapters (incl. Tom's markdown) live without becoming core.

## Repo structure

```text
agent-memory/
  README.md
  docs/
    architecture.md
    configuration.md
    privacy.md
    mcp.md
  src/agent_memory/
    daemon/
    ingest/
    store/
    retrieval/
    mcp/
    cli/
  hooks/
    git/
  launchd/
  examples/
```

## Decisions (recommended defaults)

- Single global collection + `project` field. (recommended)
- Resident LaunchAgent, single writer, warm model. (agreed)
- Git hook = enqueue-only to a spool dir; scheduled sweep as backstop. (recommended over socket IPC)
- Transcript ingest = idle-detection baseline; harness hooks optional precise ping. (recommended)
- MCP over HTTP/SSE + auth token; stdio shim only if needed. (recommended)
- `store/` as an interface with a zvec adapter. (recommended)
- v1 excludes personal-markdown import; generic importers + `examples/importers/` later. (agreed)
- Local-default embedder, bundle/auto-download model. (leaning, see open questions)

## Open questions

1. Embedding runtime: bundle a local model (A) vs API-default (B)? This blocks packaging.
2. Source of truth for the public store: zvec-only, or zvec-as-derived-index over a human-readable mirror? (Memo argues zvec owns its store; revisit only if debuggability/recovery bites.)
3. Redaction on ingest: what's the secret-scrubbing strategy and how aggressive by default?
4. MCP auth mechanism: bearer token vs unix-socket file perms?
5. Reuse qmd's already-downloaded EmbeddingGemma model, or pick our own?
6. Repo home + license + public name (`agent-memory` likely taken on PyPI — check).
