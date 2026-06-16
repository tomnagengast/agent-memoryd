# zvec

The default `agent-memoryd` build uses a pure-Go lexical index. That keeps the contributor loop simple and avoids native dependencies for ordinary tests.

The production retrieval backend uses `github.com/zvec-ai/zvec-go` behind the `zvec` build tag.

## Build

Download native zvec libraries:

```sh
mise run zvec-libs
```

Build the zvec-enabled binary:

```sh
mise run build-zvec
```

`build-zvec` sets cgo include and linker flags for the downloaded libraries and writes the binary to `./agent-memoryd`.

## Configure

Set the index backend in `$AGENT_MEMORYD_HOME/config.json`:

```json
{
  "index_backend": "zvec"
}
```

Keep the rest of the generated config fields unless you are intentionally moving the data root, source store, index path, or ingest paths.

## Rebuild

The zvec index is derived from `memories.jsonl`. Rebuild it whenever switching backends or after changing index storage:

```sh
./agent-memoryd reindex
```

## Native Library Notes

The downloaded native libraries live in `./lib`, which is ignored by git. The default `mise run build` command does not require this directory.

Supported prebuilt platforms in the current task are macOS arm64, Linux amd64, and Linux arm64. Other platforms need native zvec libraries and matching cgo flags.
