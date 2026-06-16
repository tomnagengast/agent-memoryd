# Uninstall

`agent-memoryd init` writes a resource manifest so the system can show and
remove the resources it owns.

Inspect the current system:

```sh
agent-memoryd status
```

`status` prints the loaded config, store status, command help, MCP tool help,
and all resources tracked in `resources.json`.

## Preview

Run `uninstall` without `--yes` to see the managed resources without deleting
them:

```sh
agent-memoryd uninstall
```

The command returns JSON with `needs_yes: true`.

## Remove

Remove managed resources:

```sh
agent-memoryd uninstall --yes
```

This removes the configured data root, including the config file, resource
manifest, `memories.jsonl`, zvec index directory, git spool, and logs.

If `~/Library/LaunchAgents/dev.agent-memoryd.plist` exists and is tracked by the
manifest, uninstall also tries to unload it with `launchctl bootout` and remove
the plist.

## Not Removed

`uninstall --yes` does not remove the repository checkout, built binaries
outside the data root, downloaded zvec libraries in the repository, or git hooks
you manually copied into other repositories.

It also does not touch transcript source directories such as `~/.claude` or
`~/.codex`.
