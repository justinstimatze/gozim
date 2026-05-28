# gozimmcp

A [Model Context Protocol](https://modelcontextprotocol.io) stdio server that
exposes ZIM archives (offline Wikipedia, Stack Exchange, Wiktionary, …) to LLMs.

It is a pure-Go, single-binary alternative to
[openzim-mcp](https://github.com/openzim/openzim-mcp). Because the underlying
`zim` package is memory-mapped with a bounded LRU cache and opens Bleve indexes
lazily, **resident memory stays flat regardless of query volume** — there is no
growing in-process object graph to leak.

## Build

```bash
CGO_ENABLED=0 go build -o gozimmcp ./cmd/gozimmcp
```

## Run

```bash
gozimmcp <zim-dir>
```

`<zim-dir>` is a directory of `.zim` files. Every `.zim` in it is opened and
addressed by a slug derived from its filename
(`wikipedia_en_all_2024-01.zim` → `wikipedia-en-all-2024-01`). Diagnostics go to
stderr; stdout carries the JSON-RPC stream.

## Configure in Claude Code

Add to `~/.claude.json` (or your client's MCP config):

```json
{
  "mcpServers": {
    "wikipedia-zim": {
      "command": "/abs/path/to/gozimmcp",
      "args": ["/opt/zim"],
      "type": "stdio"
    }
  }
}
```

## Tools

| Tool | Arguments | Returns |
|------|-----------|---------|
| `list_archives` | — | every archive: slug, title, language, entry count, and whether ranked fulltext search is available |
| `search` | `archive`, `query`, `limit?` | ranked fulltext hits when an index exists (`mode: "fulltext"`), otherwise title-prefix hits (`mode: "titles"`) |
| `get_article` | `archive`, `path`, `max_bytes?`, `offset?` | entry content with its mime type and an `encoding` field (`utf-8` for text — always valid, truncation aligns to rune boundaries — or `base64` for binary entries); paginate large pages via raw-byte `max_bytes`/`offset` |
| `get_metadata` | `archive` | title, language, creator, date, description, entry count, UUID |
| `main_page` | `archive` | the archive's home page path and content |

## Fulltext search

`search` returns ranked [Bleve](https://github.com/blevesearch/bleve) fulltext
results **only when a prebuilt index sits next to the ZIM file** — either
`<file>.zim.bleve` or `<file>.bleve`. Without one it falls back to fast
title-prefix search and reports `mode: "titles"`.

To build a fulltext index (one-time, can take a while for large archives):

```bash
go run ./cmd/gozimindex -path=/opt/zim/wikipedia.zim -index=/opt/zim/wikipedia.zim.bleve
```

The server never builds an index on demand — that would turn a single tool call
into a multi-minute, multi-gigabyte operation.
