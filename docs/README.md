# agent-memoryd Docs

Start with [Install](./install.md) and [Getting started](./getting-started.md) if you are trying the project for the first time.

## Guides

| Page                                    | Purpose                                                                                       |
| --------------------------------------- | --------------------------------------------------------------------------------------------- |
| [Install](./install.md)                 | Build the binary, initialize the data root, and optionally build with zvec.                   |
| [Getting started](./getting-started.md) | Try the CLI, run MCP, and run the daemon.                                                     |
| [Config](./config.md)                   | Understand `config.json`, `MEMORYD_HOME`, summarizer settings, and persisted resources. |
| [Architecture](./architecture.md)       | See how the store, index, daemon, and MCP server fit together.                                |
| [MCP](./mcp.md)                         | Configure the stdio server and use the memory tools.                                          |
| [Daemon](./daemon.md)                   | Run summarizer-driven transcript and git-event ingestion.                                     |
| [Git hooks](./git-hooks.md)             | Understand managed global hooks and git event enqueueing.                                     |
| [zvec](./zvec.md)                       | Build and configure the zvec-backed retrieval index.                                          |
| [Uninstall](./uninstall.md)             | Inspect and remove managed resources.                                                         |
| [Contributing](./contributing.md)       | Run checks and work within the repository conventions.                                        |
