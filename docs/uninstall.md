# Uninstall

`agent-memoryd init` writes a resource manifest so the system can show and remove the resources it owns. On macOS, `init` also starts the managed launchd service.

Inspect the current system:

```sh
agent-memoryd status
```

`status` prints the loaded config, store status, command help, MCP tool help, Git hook status, and all resources tracked in `resources.json`.

## Preview

Run `uninstall` without `--yes` to see the managed resources without deleting them:

```sh
agent-memoryd uninstall
```

The command returns JSON with `needs_yes: true`.

## Remove

Remove managed resources:

```sh
agent-memoryd uninstall --yes
```

This removes the configured data root, including the config file, resource manifest, zvec store directory, advisory lock file, ingest state, git spool, managed global Git hook scripts, and logs.

If `~/Library/LaunchAgents/dev.agent-memoryd.plist` exists and is tracked by the manifest, uninstall also unloads it with `launchctl bootout` and removes the plist.

If global `core.hooksPath` points at the managed hook directory, uninstall unsets that Git config value before removing the data root.

## Not Removed

`uninstall --yes` does not remove:

- The repository checkout or built binaries outside the data root.
- The native library installed to `~/.local/lib/agent-memoryd/`. Remove this manually if you no longer need it:

  ```sh
  rm -rf ~/.local/lib/agent-memoryd
  ```

- The installed binary at `~/.local/bin/agent-memoryd`. Remove it manually if desired:

  ```sh
  rm ~/.local/bin/agent-memoryd
  ```

- The daemon socket `$AGENT_MEMORYD_HOME/agent-memoryd.sock` is removed at daemon shutdown. If the daemon was killed and the socket remains, it is cleaned up automatically on next daemon start.

- Downloaded zvec libraries in the repository `./lib/` directory.
- Git hooks you manually copied into other repositories, or a different global hooks directory you configured yourself.
- Transcript source directories such as `~/.claude` or `~/.codex`.
