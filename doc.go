// Package zim reads ZIM archive files (https://wiki.openzim.org/wiki/ZIM_file_format).
//
// ZIM is an archive format used primarily for offline Wikipedia content.
// This package provides a pure Go implementation with zero CGO dependencies,
// supporting ZIM v5 and v6 formats with LZMA/XZ and Zstandard decompression.
//
// # Opening an Archive
//
//	archive, err := zim.Open("wikipedia.zim")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer archive.Close()
//
// # Entry Access
//
//	entry, err := archive.GetEntryByPath("Hard_problem_of_consciousness")
//	data, err := entry.Content()
//
// # Title Prefix Search
//
// Binary search on the title index, no external index needed:
//
//	results, err := archive.SearchTitles("quantum", 10)
//
// # Fulltext Search
//
// Builds a Bleve index on first call, persists to disk:
//
//	results, err := archive.Search("perception sensory", 10)
//
// # Iteration
//
// Range-over-func iterators for lazy traversal:
//
//	for idx, entry := range archive.Articles() {
//	    fmt.Println(idx, entry.Title())
//	}
package zim
