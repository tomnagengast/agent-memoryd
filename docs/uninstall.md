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

This removes the configured data root, including the config file, resource manifest, `memories.jsonl`, ingest state, zvec index directory, git spool, managed global Git hook scripts, and logs.

If `~/Library/LaunchAgents/dev.agent-memoryd.plist` exists and is tracked by the manifest, uninstall also unloads it with `launchctl bootout` and removes the plist.

If global `core.hooksPath` points at the managed hook directory, uninstall unsets that Git config value before removing the data root.

## Not Removed

`uninstall --yes` does not remove the repository checkout, built binaries outside the data root, downloaded zvec libraries in the repository, git hooks you manually copied into other repositories, or a different global hooks directory that you configured yourself.

It also does not touch transcript source directories such as `~/.claude` or `~/.codex`.
