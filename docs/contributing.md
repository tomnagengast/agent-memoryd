# Contributing

`agent-memoryd` is intended to be understandable to new contributors. Keep changes small, local-first, and explicit about what data is persisted.

## Development Loop

Install tools:

```sh
mise install
```

Download the native zvec libraries (required before building or running full tests):

```sh
mise run zvec-libs
```

Run the standard checks:

```sh
mise run fmt
mise run test
mise run build
```

GitHub Actions runs formatting, tests, the normal build, a GoReleaser config check, and a GoReleaser snapshot release on pushes to `main` and pull requests.

Create a GitHub release by pushing a semver tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

Tag pushes run `.github/workflows/release.yml`. The workflow runs GoReleaser on a macOS arm64 runner, builds the native cgo binary, packages the bundled zvec dylib, writes `checksums.txt`, publishes the GitHub release, and updates the `memoryd-cli` cask.

To publish the cask to the tap, configure a repository secret named `HOMEBREW_TAP_GITHUB_TOKEN` with write access to `tomnagengast/homebrew-tap`. This matches the `scout` release setup and writes the cask under `Casks/memoryd-cli.rb`.

## Project Shape

The CLI lives in `internal/app`.

The durable memory model and zvec-backed store live in `internal/memory`.

Config and lifecycle resources live in `internal/config`.

Daemon ingestion lives in `internal/daemon`, `internal/ingest`, and `internal/spool`.

The configurable summarization adapter lives in `internal/summarizer`.

The configurable embedding adapter lives in `internal/embedder`.

The IPC server and client (daemon socket) live in `internal/storerpc`.

## Build Details

`mise run build` always uses `CGO_ENABLED=1` and links against the zvec native library in `./lib/`. There is no pure-Go fallback build. `mise run zvec-libs` must be run before `mise run build`.

`mise run install-local` builds the binary with an rpath pointing at `~/.local/lib/memoryd/` (not the working tree `./lib/` directory) and copies the native library there before installing the binary to `~/.local/bin/`. This makes the installed binary independent of the repository checkout location.

`.goreleaser.yaml` is the release source of truth. The build hook downloads the macOS arm64 zvec library, and `scripts/clean-darwin-rpaths.sh` removes the absolute zvec module-cache rpath after the build so the release binary loads `libzvec_c_api.dylib` from the staged cask/archive directory.

## Design Principles

Keep the zvec store as the sole durable memory store. The store is the system of record, not a derived cache.

Keep MCP tools compact. `search` should return summaries and ids; `get` should return full records only when needed. `reflect` should store distilled memories through the summarizer, not raw session text.

Do not add migration code from a private memory system. Public contributors should see a clean install path.

Prefer local behavior over hosted services. External dependencies should be optional unless they are core to retrieval.

Daemon and MCP reflection producers should not store raw transcripts, session text, tool logs, diffs, or git output as memory bodies. Pass source material to the summarizer and store only distilled memories with `source` and `More detail:` references.

## Documentation

Update `README.md` and the relevant file in `docs/` when changing commands, config fields, managed resources, MCP tools, ingestion behavior, summarizer behavior, or embedder/search behavior.

Docs should describe current behavior plainly. If something is a future direction, mark it as such or leave it out.
