[![Build status](https://github.com/justinstimatze/gozim/actions/workflows/build.yml/badge.svg)](https://github.com/justinstimatze/gozim/actions/workflows/build.yml)
[![Lint status](https://github.com/justinstimatze/gozim/actions/workflows/lint.yml/badge.svg)](https://github.com/justinstimatze/gozim/actions/workflows/lint.yml)

# gozim

Pure Go ZIM file reader with fulltext search. Zero CGO. Zero Python. Zero HTTP.

**The problem:** AI agents and Go tools that need Wikipedia access typically call an API (rate-limited, requires internet) or shell out to Python/C++ (fragile, hard to deploy). ZIM files give you all of Wikipedia in a single 90GB file — but until now, reading them from Go meant CGO bindings to libzim or external processes.

**gozim** is a pure Go library that opens ZIM files directly. Title prefix search works instantly via binary search on the built-in index. Fulltext search builds a [Bleve](https://github.com/blevesearch/bleve) index on first use and persists it to disk. No network, no subprocesses, no CGO. Cross-compiles to any platform Go targets.

## Use cases

- **AI agent tools** — give LLM agents local Wikipedia access via MCP servers or function calling, with zero latency and no rate limits
- **RAG retrieval** — offline Wikipedia as a retrieval corpus for retrieval-augmented generation pipelines
- **Knowledge systems** — structured access to Wikipedia for research tools, fact-checking, knowledge graph construction
- **Edge/embedded** — serve Wikipedia on a Raspberry Pi, air-gapped network, or any device Go compiles for

## Install

```bash
go get github.com/justinstimatze/gozim
```

Download ZIM files from [download.kiwix.org/zim/](https://download.kiwix.org/zim/).

## Usage

```go
archive, err := zim.Open("wikipedia_en_all.zim")
defer archive.Close()

// Direct article access
entry, _ := archive.GetEntryByPath("Hard_problem_of_consciousness")
data, _ := entry.Content()

// Archive metadata
fmt.Println(archive.Title(), archive.Language())

// Title prefix search — binary search, no index needed
results, _ := archive.SearchTitles("apophenia", 10)

// Fulltext search — builds Bleve index on first call, persists to disk
results, _ := archive.Search("perception sensory over-interpretation", 10)

// Iterate all articles
for idx, entry := range archive.Articles() {
    fmt.Println(idx, entry.Title())
}
```

## Features

- ZIM v5 and v6 format support
- Zstandard decompression (pure Go)
- Title prefix search via binary search on title index
- Fulltext search via Bleve with lazy index building
- Structured metadata (Title, Language, Creator, Date, etc.)
- `iter.Seq2` iterators with early-break support
- MD5 checksum validation
- Memory-mapped and streaming file access
- Context-aware index building with cancellation

## API

| Method | Description |
|--------|-------------|
| `Open(path, ...Option)` | Open a ZIM file |
| `GetEntryByPath(path)` | Find entry by path (article namespace) |
| `GetEntryByFullPath(path)` | Find entry by namespace/path |
| `GetEntryByIndex(idx)` | Entry at URL index position |
| `MainEntry()` | Main page entry |
| `SearchTitles(prefix, limit)` | Title prefix search (no index needed) |
| `Search(query, limit, ...SearchOption)` | Fulltext search via Bleve |
| `BuildIndex(...SearchOption)` | Build search index without searching |
| `Title()`, `Language()`, `Creator()`, `Date()`, `Description()` | Metadata |
| `Metadata(key)` | Arbitrary metadata key |
| `ValidateChecksum()` | MD5 integrity check |
| `Entries()`, `Articles()`, `EntriesByTitle()`, `EntriesInNamespace(ns)` | Iterators |
| `EntryCount()`, `ClusterCount()`, `UUID()`, `MimeTypes()` | Archive info |

### Options

```go
zim.Open(path, zim.WithMmap(), zim.WithBlobCacheSize(10))

archive.Search(query, limit,
    zim.WithIndexPath("/tmp/wiki.bleve"),
    zim.WithAnalyzer("en"),
    zim.WithContentIndexing(extractText),
    zim.WithOffset(20),
    zim.WithContext(ctx),
)
```

## Build

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./...
```

## Tools

**gozimindex** — Build a fulltext search index:
```bash
go run ./cmd/gozimindex -path=wikipedia.zim -index=wikipedia.idx
```

**gozimhttpd** — Serve ZIM content over HTTP:
```bash
go run ./cmd/gozimhttpd -path=wikipedia.zim [-index=wikipedia.idx]
```

## License

MIT
