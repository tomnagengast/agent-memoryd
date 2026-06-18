# Getting Started

This guide walks through a local install that an agent can use over MCP.

## Build And Initialize

```sh
mise install
mise run build
./memoryd --help
./memoryd init
./memoryd status
```

In an interactive terminal, `init` walks through onboarding choices: start fresh or import existing memories, enable default transcript ingestion roots, configure Ollama semantic search, and start the daemon service now. For non-interactive installs, use `./memoryd init --fresh` to skip prompts or `./memoryd init --import ~/notes/agent` to import an existing JSONL file or markdown/text directory.

`init` installs managed global Git hooks when no other global hook path is configured. On macOS, it also installs and starts the managed launchd daemon. The daemon begins polling the configured transcript roots and git event spool immediately. Use `./memoryd init --no-daemon` to create files and Git hooks without starting the background service.

`status` prints JSON with command help, MCP tool help, the loaded config, store status, launchd service status, Git hook status, and every resource tracked by the `init` manifest.

## Try The CLI

Add a memory:

```sh
./memoryd add \
  --kind fact \
  --project example \
  --summary "Agent memory stores durable local notes" \
  "agent-memoryd stores durable records in zvec and serves them through the daemon."
```

Search summaries:

```sh
./memoryd search --project example "durable local notes"
```

Explore memories interactively:

```sh
./memoryd explore
```

Fetch the full memory by id:

```sh
./memoryd get <memory-id>
```

Delete a memory:

```sh
./memoryd forget <memory-id>
```

## Run MCP

Run the stdio MCP server:

```sh
./memoryd mcp
```

Configure your MCP client to launch the binary with the `mcp` argument. A typical client entry looks like:

```json
{
  "command": "/absolute/path/to/memoryd",
  "args": ["mcp"],
  "env": {
    "MEMORYD_HOME": "/Users/you/.local/share/memoryd"
  }
}
```

## Run The Daemon Manually

`init` starts the daemon through launchd on macOS. To run the daemon in the foreground instead:

```sh
./memoryd daemon
```

The default daemon summarizer uses `codex exec`. Edit `summarizer_command` in `config.json` if you want another local summarization agent.

For a one-shot ingest pass without staying resident:

```sh
./memoryd scan-once
```

The daemon waits until transcript files are idle before passing them to the summarizer. See [daemon.md](./daemon.md) for ingestion details.

## Enable Semantic Search

Full-text search works without an embedder. To add local semantic search with Ollama:

```sh
ollama pull nomic-embed-text
./memoryd embedder setup ollama
./memoryd embedder test
./memoryd reindex
```

Restart the daemon after changing embedder config so new writes use the provider.
