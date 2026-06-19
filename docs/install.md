# Install

`agent-memoryd` is distributed as a single Go binary named `memoryd` that links against a native zvec library. The repository uses `mise` to pin the Go toolchain and expose the common development tasks.

## System Requirements

| Requirement        | Notes                                                                                                                              |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------------- |
| Go                 | Managed by `mise`; see `.mise.toml` for the pinned version.                                                                        |
| macOS              | Homebrew and release archives are built for macOS arm64.                                                                           |
| C toolchain        | Required because the build uses cgo to link the zvec native library.                                                               |
| Git                | Optional, but required for git hook ingestion and commit source material.                                                          |
| Summarizer command | Required for daemon-generated transcript and git memories. The default config uses `codex exec`.                                   |

macOS Intel is not packaged until zvec publishes a `darwin_amd64` native library. Linux remains supported when building locally, but the Homebrew release path currently follows the macOS arm64 cask used by the release workflow.

## Install With Homebrew

Install from the tap:

```sh
brew install --cask tomnagengast/tap/memoryd-cli
memoryd --version
memoryd init
```

The cask installs `memoryd` and stages the bundled zvec native library beside it. Run `memoryd init` after installation to create the local data root, config, Git hooks, and the managed daemon service.

## Build From Source

Download the native zvec libraries (required before building):

```sh
mise run zvec-libs
```

This fetches prebuilt libraries into `./lib/`. Build the binary:

```sh
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

## Install Locally

`mise run install-local` builds a tree-independent binary (rpath pointing at `~/.local/lib/memoryd`) and installs both the binary and the native library:

```sh
mise run zvec-libs   # if not already done
mise run install-local
```

This copies `libzvec_c_api.dylib` (or `.so` on Linux) to `~/.local/lib/memoryd/` and installs the binary to `~/.local/bin/memoryd` with an atomic replace. After install, the binary loads the native library from `~/.local/lib/memoryd/` regardless of whether the repository working tree is present.

Release builds can set `MEMORYD_VERSION`, usually to a semver tag such as `v0.1.0`. Without that override, the build task uses `git describe --tags --always --dirty`.

Then initialize:

```sh
memoryd init
```

## Install From A GitHub Release

Release assets include `memoryd` and `libzvec_c_api.dylib` at the archive root. Choose a tag and install the matching asset:

```sh
version="v0.1.0"
asset_version="${version#v}"
os="darwin"
arch="arm64"

curl -L \
  -o /tmp/agent-memoryd.tar.gz \
  "https://github.com/tomnagengast/agent-memoryd/releases/download/${version}/agent-memoryd_${asset_version}_${os}_${arch}.tar.gz"
mkdir -p ~/.local/bin
tar -xzf /tmp/agent-memoryd.tar.gz -C /tmp memoryd libzvec_c_api.dylib
install -m 755 /tmp/memoryd ~/.local/bin/memoryd
install -m 644 /tmp/libzvec_c_api.dylib ~/.local/bin/libzvec_c_api.dylib
memoryd --version
memoryd init
```

Each release also includes `checksums.txt`. The matching Homebrew cask is published to `tomnagengast/homebrew-tap` by GoReleaser when tap credentials are configured.

## Initialize

Create the local data root, default config, zvec store, git spool, managed global Git hooks, logs directory, resource manifest, and managed daemon service:

```sh
./memoryd init
```

In an interactive terminal, `init` walks through onboarding choices: start fresh or import existing memories, enable default transcript ingestion roots, configure Ollama semantic search, and start the daemon service now. Non-interactive installs should pass one of these flags:

```sh
./memoryd init --fresh
./memoryd init --import ~/notes/agent
./memoryd init --import ~/.local/share/memoryd/memories.jsonl
```

The import path may be an agent-memoryd JSONL store, a markdown file, a text file, or a directory containing markdown/text files. JSONL records keep their existing ids, kinds, projects, sources, summaries, and bodies. Markdown and text imports become `note` records with stable `import:<hash>` ids and source paths, so running the same import again updates the same records instead of duplicating them.

`init` writes executable hooks under `~/.local/share/memoryd/git-hooks` and sets `git config --global core.hooksPath` to that directory when the global value is unset or already points at the managed directory. If another global hook path is already configured, `init` leaves it alone and reports that in `git_hooks`.

On macOS, `init` writes `~/Library/LaunchAgents/dev.memoryd.plist`, bootstraps it with launchd, and kickstarts `memoryd daemon`. On other platforms, launchd setup is skipped.

Use `--no-daemon` if you only want to create the local files and Git hooks without starting the daemon service:

```sh
./memoryd init --no-daemon
```

Interactive `init` can configure local semantic search with Ollama. The equivalent scripted setup is:

```sh
ollama pull nomic-embed-text
./memoryd embedder setup ollama
./memoryd embedder test
./memoryd reindex
```

Restart the daemon after changing embedder config.

By default the data root is:

```text
~/.local/share/memoryd
```

Set `MEMORYD_HOME` before running commands to use another root:

```sh
MEMORYD_HOME=/tmp/memoryd ./memoryd init
```
