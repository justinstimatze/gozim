[![Build status](https://github.com/justinstimatze/gozim/actions/workflows/build.yml/badge.svg)](https://github.com/justinstimatze/gozim/actions/workflows/build.yml)
[![Lint status](https://github.com/justinstimatze/gozim/actions/workflows/lint.yml/badge.svg)](https://github.com/justinstimatze/gozim/actions/workflows/lint.yml)

gozim
=====

A pure Go implementation for reading ZIM files. Zero CGO.

ZIM files are used mainly as offline Wikipedia copies.
See the [ZIM format spec](https://wiki.openzim.org/wiki/ZIM_file_format) and [download ZIM files](https://download.kiwix.org/zim/).

## Install

```bash
go get github.com/justinstimatze/gozim
```

## Build

```bash
CGO_ENABLED=0 go build ./...
```

## Test

```bash
CGO_ENABLED=0 go test ./...
```

## Tools

### gozimindex

Build a Bleve fulltext index from a ZIM file:

```bash
go build ./cmd/gozimindex
./gozimindex -path=yourzimfile.zim -index=yourzimfile.idx
```

### gozimhttpd

Serve ZIM content over HTTP with optional search:

```bash
go build ./cmd/gozimhttpd
./gozimhttpd -path=yourzimfile.zim [-index=yourzimfile.idx]
```

![Browse](/shots/browse.jpg)
![Search](/shots/search.jpg)

## License

MIT
