// Command gozimmcp is a stdio MCP server that exposes ZIM archives (offline
// Wikipedia, Stack Exchange, etc.) to LLMs as a set of tools: list archives,
// search, fetch articles, and read metadata.
//
// It is a pure-Go single-binary replacement for openzim-mcp. Because the
// underlying zim package is mmap-backed with a bounded LRU cache and opens
// Bleve indexes lazily, resident memory stays flat regardless of query volume.
//
// Usage:
//
//	gozimmcp <zim-dir>
//
// where <zim-dir> is a directory of .zim files (scanned recursively by
// OpenLibrary). Diagnostics go to stderr; stdout carries the JSON-RPC stream.
package main

import (
	"context"
	"fmt"
	"os"

	zim "github.com/justinstimatze/gozim"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is reported to MCP clients in the initialize handshake.
const version = "0.1.0"

// lib holds the opened archive library for the lifetime of the process.
// Tool handlers read it; it is never mutated after startup.
var lib *zim.Library

func main() {
	if len(os.Args) != 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Fprintln(os.Stderr, "usage: gozimmcp <zim-dir>")
		os.Exit(2)
	}
	dir := os.Args[1]

	l, err := zim.OpenLibrary(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gozimmcp: open library %q: %v\n", dir, err)
		os.Exit(1)
	}
	lib = l
	defer lib.Close()

	// Per-file open failures are non-fatal — report them but keep serving the
	// archives that did load.
	for _, e := range lib.Errors() {
		fmt.Fprintf(os.Stderr, "gozimmcp: archive load warning: %v\n", e)
	}
	fmt.Fprintf(os.Stderr, "gozimmcp: serving %d archive(s) from %s\n", lib.Len(), dir)

	server := mcp.NewServer(&mcp.Implementation{Name: "gozimmcp", Version: version}, nil)
	registerTools(server)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "gozimmcp: server error: %v\n", err)
		os.Exit(1)
	}
}
