[![Build status](https://github.com/justinstimatze/gozim/actions/workflows/build.yml/badge.svg)](https://github.com/justinstimatze/gozim/actions/workflows/build.yml)
[![Lint status](https://github.com/justinstimatze/gozim/actions/workflows/lint.yml/badge.svg)](https://github.com/justinstimatze/gozim/actions/workflows/lint.yml)

# gozim

Pure Go library for reading [ZIM files](https://wiki.openzim.org/wiki/ZIM_file_format). Zero CGO.

Supports ZIM v5 and v6, LZMA/XZ and Zstandard decompression, title prefix search, fulltext search via [Bleve](https://github.com/blevesearch/bleve), and structured metadata access.

## Install

```bash
go get github.com/justinstimatze/gozim
```

## Usage

```go
archive, err := zim.Open("wikipedia.zim")
defer archive.Close()

// Entry access
entry, _ := archive.GetEntryByPath("Hard_problem_of_consciousness")
fmt.Println(entry.Title(), len(entry.Content()))

// Metadata
fmt.Println(archive.Title(), archive.Language())

// Title prefix search (binary search, no index needed)
results, _ := archive.SearchTitles("apophenia", 10)

// Fulltext search (builds Bleve index on first call)
results, _ := archive.Search("perception sensory", 10)

// Iterate all articles
for idx, entry := range archive.Articles() {
    fmt.Println(idx, entry.Title())
}

// Checksum validation
valid, _ := archive.ValidateChecksum()
```

## API

| Method | Description |
|--------|-------------|
| `Open(path, ...Option)` | Open a ZIM file |
| `GetEntryByPath(path)` | Find entry by path |
| `GetEntryByFullPath(path)` | Find entry by namespace/path |
| `GetEntryByIndex(idx)` | Entry at URL index position |
| `MainEntry()` | Main page entry |
| `SearchTitles(prefix, limit)` | Title prefix search |
| `Search(query, limit, ...SearchOption)` | Fulltext search via Bleve |
| `Title()`, `Language()`, `Creator()`, `Date()`, `Description()` | Metadata |
| `Metadata(key)` | Arbitrary metadata key |
| `ValidateChecksum()` | MD5 integrity check |
| `Entries()`, `Articles()`, `EntriesByTitle()`, `EntriesInNamespace(ns)` | Iterators |
| `EntryCount()`, `ClusterCount()`, `UUID()`, `MimeTypes()` | Archive info |

## Build

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./...
```

## Tools

**gozimindex** — Build a fulltext search index:
```bash
go run ./cmd/gozimindex -path=file.zim -index=file.idx
```

**gozimhttpd** — Serve ZIM content over HTTP:
```bash
go run ./cmd/gozimhttpd -path=file.zim [-index=file.idx]
```

## License

MIT
