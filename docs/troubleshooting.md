# Troubleshooting

Start with:

```sh
memoryd status
```

`status` reports command help, MCP tool help, loaded config, store status, launchd service status, Git hook status, and managed resource paths.

## Daemon Not Running

Store commands route through the daemon socket at `$MEMORYD_HOME/memoryd.sock`. If commands fail because the daemon is unavailable, start it through the managed service or run it in the foreground:

```sh
memoryd daemon
```

For one pass without staying resident:

```sh
memoryd scan-once
```

## launchd Issues

On macOS, `memoryd init` writes:

```text
~/Library/LaunchAgents/dev.memoryd.plist
```

Inspect the rendered plist without installing it:

```sh
memoryd launchd-plist --bin "$(command -v memoryd)"
```

Daemon logs live under:

```text
$MEMORYD_HOME/logs
```

## Git Hooks Not Enqueueing

Check the configured global hook path:

```sh
git config --global --get core.hooksPath
```

`memoryd init` only sets `core.hooksPath` when it is unset or already points at the managed hook directory. If another global hook path is configured, chain to the managed hooks manually or enqueue a commit directly:

```sh
memoryd enqueue-git \
  --repo "$(git rev-parse --show-toplevel)" \
  --sha "$(git rev-parse HEAD)"
```

## Ollama Embedder Setup

Full-text search works without an embedder. To enable local semantic search:

```sh
ollama pull nomic-embed-text
memoryd embedder setup ollama
memoryd embedder test
memoryd reindex
```

Restart the daemon after changing embedder config.

## Native Library Loading

Source builds require the zvec native library:

```sh
mise run zvec-libs
mise run build
```

Local installs copy the native library to `~/.local/lib/memoryd/` and rebuild the binary with an rpath pointing there:

```sh
mise run install-local
```

## Stale Socket

The daemon removes `$MEMORYD_HOME/memoryd.sock` on shutdown. If the daemon was killed and the socket remains, the next daemon start cleans it up automatically.
