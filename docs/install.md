# Install

`agent-memoryd` is distributed as a single Go binary. The repository uses `mise` to pin the Go toolchain and expose the common development tasks.

## System Requirements

| Requirement | Notes |
| --- | --- |
| Go | Managed by `mise`; see `.mise.toml` for the pinned version. |
| macOS or Linux | The default lexical build is pure Go. The zvec build currently has prebuilt native library support for macOS arm64 and Linux amd64/arm64. |
| Git | Optional, but required for git hook ingestion and commit source material. |
| Summarizer command | Required for daemon-generated transcript and git memories. The default config uses `codex exec`. |
| C toolchain | Required only for the `zvec` build tag because `zvec-go` uses cgo. |

## Build From Source

Install project tools and build the default binary:

```sh
mise install
mise run build
```

The binary is written to `./agent-memoryd`.

Verify the build:

```sh
./agent-memoryd --help
./agent-memoryd --version
```

`--help` does not require an initialized data root or config file. After initialization, `status` reports the planned and existing resource paths for the configured data root.

`--version` and `-v` print the version, commit, and build time stamped by the `mise run build` task. Compare the repository binary with the installed binary to see whether your global copy needs refreshing:

```sh
./agent-memoryd --version
agent-memoryd --version
```

Update the installed binary with an atomic replace so macOS does not kill executions of a launchd-managed binary that was overwritten in place:

```sh
mise run install-local
agent-memoryd init
```

Release builds can set `AGENT_MEMORYD_VERSION`, usually to a semver tag such as `v0.1.0`. Without that override, the build task uses `git describe --tags --always --dirty`.

## Initialize

Create the local data root, default config, memory store, git spool, logs directory, resource manifest, and managed daemon service:

```sh
./agent-memoryd init
```

On macOS, `init` writes `~/Library/LaunchAgents/dev.agent-memoryd.plist`, bootstraps it with launchd, and kickstarts `agent-memoryd daemon`. On other platforms, launchd setup is skipped.

Use `--no-daemon` if you only want to create the local files:

```sh
./agent-memoryd init --no-daemon
```

By default the data root is:

```text
~/.local/share/agent-memoryd
```

Set `AGENT_MEMORYD_HOME` before running commands to use another root:

```sh
AGENT_MEMORYD_HOME=/tmp/agent-memoryd ./agent-memoryd init
```

## Optional zvec Build

The default build uses a pure-Go lexical index so contributors can run tests without native dependencies. The production retrieval path is available behind the `zvec` build tag.

Download the zvec native libraries and build the tagged binary:

```sh
mise run zvec-libs
mise run build-zvec
```

Then set `index_backend` to `zvec` in the config file and rebuild the index:

```sh
./agent-memoryd reindex
```

See [zvec.md](./zvec.md) for details.
