# Getting Started

This guide walks through a local install that an agent can use over MCP.

## Build And Initialize

```sh
mise install
mise run build
./agent-memoryd init
./agent-memoryd status
```

`status` prints JSON with command help, MCP tool help, the loaded config, store
status, and every resource tracked by the `init` manifest.

## Try The CLI

Add a memory:

```sh
./agent-memoryd add \
  --kind fact \
  --project example \
  --summary "Agent memory stores durable local notes" \
  "agent-memoryd stores source records in JSONL and rebuilds its retrieval index."
```

Search summaries:

```sh
./agent-memoryd search --project example "durable local notes"
```

Fetch the full memory by id:

```sh
./agent-memoryd get <memory-id>
```

Delete a memory:

```sh
./agent-memoryd forget <memory-id>
```

## Run MCP

Run the stdio MCP server:

```sh
./agent-memoryd mcp
```

Configure your MCP client to launch the binary with the `mcp` argument. A
typical client entry looks like:

```json
{
  "command": "/absolute/path/to/agent-memoryd",
  "args": ["mcp"],
  "env": {
    "AGENT_MEMORYD_HOME": "/Users/you/.local/share/agent-memoryd"
  }
}
```

## Run The Daemon

The daemon polls configured transcript roots and the git event spool:

```sh
./agent-memoryd daemon
```

For a one-shot ingest pass:

```sh
./agent-memoryd scan-once
```

The daemon waits until transcript files are idle before indexing them. See
[daemon.md](./daemon.md) for ingestion details.
