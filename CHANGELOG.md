# Changelog

All notable changes to `agent-memoryd` are tracked here.

## v0.1.1 - 2026-06-19

- Adds Homebrew cask release packaging and CI release snapshot fixes.
- Fixes local MCP launcher behavior and zvec release rpath handling.
- Adds shared repo-quality scaffolding: license, security policy, changelog, Dependabot, issue templates, pull request template, CODEOWNERS, release docs, and troubleshooting docs.
- Updates agent instructions to document zvec as the durable source of truth.

## v0.1.0 - 2026-06-19

- Adds the `memoryd` CLI, stdio MCP server, and resident ingest daemon.
- Adds zvec-backed durable memory storage with hybrid full-text and vector retrieval.
- Adds managed initialization, launchd service setup, Git hook event enqueueing, transcript ingestion, and uninstall support.
- Adds GoReleaser packaging with checksummed release archives and the `memoryd-cli` Homebrew cask.
