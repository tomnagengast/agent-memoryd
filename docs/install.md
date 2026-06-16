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
```

`--help` does not require an initialized data root or config file. After initialization, `status` reports the planned and existing resource paths for the configured data root.

## Initialize

Create the local data root, default config, memory store, git spool, logs directory, and resource manifest:

```sh
./agent-memoryd init
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
