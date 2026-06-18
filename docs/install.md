# Install

`agent-memoryd` is distributed as a single Go binary named `memoryd`. The repository uses `mise` to pin the Go toolchain and expose the common development tasks.

## System Requirements

| Requirement        | Notes                                                                                                                                     |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------- |
| Go                 | Managed by `mise`; see `.mise.toml` for the pinned version.                                                                               |
| macOS or Linux     | The default lexical build is pure Go. The zvec build currently has prebuilt native library support for macOS arm64 and Linux amd64/arm64. |
| Git                | Optional, but required for git hook ingestion and commit source material.                                                                 |
| Summarizer command | Required for daemon-generated transcript and git memories. The default config uses `codex exec`.                                          |
| C toolchain        | Required only for the `zvec` build tag because `zvec-go` uses cgo.                                                                        |

## Build From Source

Install project tools and build the default binary:

```sh
mise install
mise run build
```

The binary is written to `./memoryd`.

Verify the build:

```sh
./memoryd --help
./memoryd --version
```

`--help` does not require an initialized data root or config file. After initialization, `status` reports the planned and existing resource paths for the configured data root.

`--version` and `-v` print the version, commit, and build time stamped by the `mise run build` task. Compare the repository binary with the installed binary to see whether your global copy needs refreshing:

```sh
./memoryd --version
memoryd --version
```

Update the installed binary with an atomic replace so macOS does not kill executions of a launchd-managed binary that was overwritten in place:

```sh
mise run install-local
memoryd init
```

Release builds can set `AGENT_MEMORYD_VERSION`, usually to a semver tag such as `v0.1.0`. Without that override, the build task uses `git describe --tags --always --dirty`.

## Install From A GitHub Release

Release assets are published for the default lexical build on macOS and Linux, for amd64 and arm64. Choose a tag and install the matching asset:

```sh
version="v0.1.0"
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64) arch="amd64" ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

curl -L \
  -o /tmp/agent-memoryd.tar.gz \
  "https://github.com/tomnagengast/agent-memoryd/releases/download/${version}/agent-memoryd_${version}_${os}_${arch}.tar.gz"
tar -xzf /tmp/agent-memoryd.tar.gz -C /tmp memoryd
mkdir -p ~/.local/bin
install -m 755 /tmp/memoryd ~/.local/bin/memoryd
memoryd --version
memoryd init
```

Each release also includes `checksums.txt`.

## Initialize

Create the local data root, default config, memory store, git spool, managed global Git hooks, logs directory, resource manifest, and managed daemon service:

```sh
./memoryd init
```

In an interactive terminal, `init` asks whether to start fresh or import existing memories. Non-interactive installs should pass one of these flags:

```sh
./memoryd init --fresh
./memoryd init --import ~/notes/agent
./memoryd init --import ~/.local/share/agent-memoryd/memories.jsonl
```

The import path may be an agent-memoryd JSONL store, a markdown file, a text file, or a directory containing markdown/text files. JSONL records keep their existing ids, kinds, projects, sources, summaries, and bodies. Markdown and text imports become `note` records with stable `import:<hash>` ids and source paths, so running the same import again updates the same records instead of duplicating them.

`init` writes executable hooks under `~/.local/share/agent-memoryd/git-hooks` and sets `git config --global core.hooksPath` to that directory when the global value is unset or already points at the managed directory. If another global hook path is already configured, `init` leaves it alone and reports that in `git_hooks`.

On macOS, `init` writes `~/Library/LaunchAgents/dev.agent-memoryd.plist`, bootstraps it with launchd, and kickstarts `memoryd daemon`. On other platforms, launchd setup is skipped.

Use `--no-daemon` if you only want to create the local files and Git hooks without starting the daemon service:

```sh
./memoryd init --no-daemon
```

By default the data root is:

```text
~/.local/share/agent-memoryd
```

Set `AGENT_MEMORYD_HOME` before running commands to use another root:

```sh
AGENT_MEMORYD_HOME=/tmp/agent-memoryd ./memoryd init
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
./memoryd reindex
```

See [zvec.md](./zvec.md) for details.
