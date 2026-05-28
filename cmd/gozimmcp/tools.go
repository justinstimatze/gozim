package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	zim "github.com/justinstimatze/gozim"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultSearchLimit / defaultMaxBytes bound results when the caller omits them.
const (
	defaultSearchLimit = 20
	defaultMaxBytes    = 100_000 // ~100 KB keeps a single article from flooding context
	maxRedirectDepth   = 10
)

// registerTools wires every tool handler onto the server. Input and output
// JSON schemas are inferred by the SDK from the In/Out struct types and their
// `jsonschema` tags.
func registerTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_archives",
		Description: "List the ZIM archives available on this server, with title, language, entry count, and whether ranked fulltext search is available (vs title-only).",
	}, handleListArchives)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Search an archive. Uses ranked Bleve fulltext when a prebuilt index exists for the archive; otherwise falls back to title-prefix search. The returned 'mode' field says which was used.",
	}, handleSearch)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_article",
		Description: "Fetch the content of a single entry by its path within an archive. Follows redirects. Content is returned as text (HTML for articles); use max_bytes/offset to paginate large pages.",
	}, handleGetArticle)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_metadata",
		Description: "Return an archive's metadata: title, language, creator, date, description, entry count, and UUID.",
	}, handleGetMetadata)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "main_page",
		Description: "Return the path and content of an archive's main (home) page.",
	}, handleMainPage)
}

// toolErr builds a tool-level error result (visible to the model) rather than a
// protocol error, per the MCP spec.
func toolErr[Out any](format string, args ...any) (*mcp.CallToolResult, Out, error) {
	var zero Out
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}, zero, nil
}

// resolveArchive looks up a library entry by slug, returning a tool error
// result if the slug is unknown.
func resolveArchive[Out any](slug string) (*zim.LibraryEntry, *mcp.CallToolResult, Out, bool) {
	e, ok := lib.Get(slug)
	if !ok {
		res, out, _ := toolErr[Out]("unknown archive %q — call list_archives for valid slugs", slug)
		return nil, res, out, false
	}
	return e, nil, *new(Out), true
}

// ---- list_archives ----

type archiveInfo struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Language    string `json:"language,omitempty"`
	Creator     string `json:"creator,omitempty"`
	Description string `json:"description,omitempty"`
	EntryCount  uint32 `json:"entry_count"`
	Fulltext    bool   `json:"fulltext"` // ranked Bleve search available?
}

type listArchivesOut struct {
	Archives []archiveInfo `json:"archives"`
}

func handleListArchives(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listArchivesOut, error) {
	var out listArchivesOut
	for _, slug := range lib.Slugs() {
		e, ok := lib.Get(slug)
		if !ok {
			continue
		}
		a := e.Archive
		out.Archives = append(out.Archives, archiveInfo{
			Slug:        slug,
			Title:       a.Title(),
			Language:    a.Language(),
			Creator:     a.Creator(),
			Description: a.Description(),
			EntryCount:  a.EntryCount(),
			Fulltext:    e.IndexPath != "",
		})
	}
	return nil, out, nil
}

// ---- search ----

type searchIn struct {
	Archive string `json:"archive" jsonschema:"slug of the archive to search (from list_archives)"`
	Query   string `json:"query" jsonschema:"the search query"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum number of results (default 20)"`
}

type searchHit struct {
	Path  string  `json:"path"`
	Title string  `json:"title"`
	Score float64 `json:"score,omitempty"` // only set in fulltext mode
}

type searchOut struct {
	Archive string      `json:"archive"`
	Mode    string      `json:"mode"` // "fulltext" or "titles"
	Hits    []searchHit `json:"hits"`
}

func handleSearch(_ context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
	e, errRes, errOut, ok := resolveArchive[searchOut](in.Archive)
	if !ok {
		return errRes, errOut, nil
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	out := searchOut{Archive: in.Archive}

	// CRITICAL: only call Search() when a prebuilt index exists. With an empty
	// index path Archive.Search would fall through to buildIndex — a
	// multi-minute, multi-GB operation. IndexPath is only set after a successful
	// stat (discoverIndex), so a non-empty value is safe to use directly.
	if e.IndexPath != "" {
		out.Mode = "fulltext"
		results, err := e.Archive.Search(in.Query, limit, zim.WithIndexPath(e.IndexPath))
		if err != nil {
			return toolErr[searchOut]("fulltext search failed: %v", err)
		}
		for _, r := range results {
			out.Hits = append(out.Hits, searchHit{
				Path:  r.Entry.Path(),
				Title: r.Entry.Title(),
				Score: r.Score,
			})
		}
		return nil, out, nil
	}

	out.Mode = "titles"
	entries, err := e.Archive.SearchTitles(in.Query, limit)
	if err != nil {
		return toolErr[searchOut]("title search failed: %v", err)
	}
	for _, ent := range entries {
		out.Hits = append(out.Hits, searchHit{Path: ent.Path(), Title: ent.Title()})
	}
	return nil, out, nil
}

// ---- get_article ----

type getArticleIn struct {
	Archive  string `json:"archive" jsonschema:"slug of the archive (from list_archives)"`
	Path     string `json:"path" jsonschema:"entry path within the archive (the 'path' field from search results)"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"maximum bytes of content to return (default 100000; 0 means no limit)"`
	Offset   int    `json:"offset,omitempty" jsonschema:"byte offset into the content to start from, for paginating large pages"`
}

type getArticleOut struct {
	Archive   string `json:"archive"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	MimeType  string `json:"mime_type"`
	Content   string `json:"content"`
	Encoding  string `json:"encoding"`   // "utf-8" for text, "base64" for binary entries
	Offset    int    `json:"offset"`     // start offset actually used (raw bytes; may advance to a rune boundary)
	TotalSize int    `json:"total_size"` // full content size in raw bytes
	Truncated bool   `json:"truncated"`  // true if more content follows past this slice
}

// isTextual reports whether a ZIM entry's mime type holds UTF-8 text safe to
// return as a string. HTML, XML/SVG/XHTML, JSON, JS and CSS all qualify.
func isTextual(mime string) bool {
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	return strings.Contains(mime, "xml") ||
		strings.Contains(mime, "html") ||
		strings.Contains(mime, "json") ||
		strings.Contains(mime, "javascript")
}

// clipContent slices content for pagination from offset, limited to maxBytes
// (0 = no limit). offset and maxBytes count raw bytes. For textual content both
// ends are aligned to UTF-8 rune boundaries so the result is always valid UTF-8
// (no mojibake at a truncation point); binary content is base64-encoded
// instead. It returns the (possibly advanced) start offset, the encoded string,
// the encoding label, and whether more content follows past the returned slice.
func clipContent(content []byte, offset, maxBytes int, textual bool) (int, string, string, bool) {
	total := len(content)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if textual {
		// A previous page may have ended mid-rune; advance past any partial
		// rune so this slice starts on a boundary.
		for offset < total && !utf8.RuneStart(content[offset]) {
			offset++
		}
	}

	slice := content[offset:]
	truncated := false
	if maxBytes > 0 && len(slice) > maxBytes {
		slice = slice[:maxBytes]
		truncated = true
		if textual {
			// Back off to the last complete rune. DecodeLastRune returns
			// (RuneError, 1) for an incomplete trailing sequence; a genuine
			// U+FFFD in the content has size 3, so size==1 disambiguates.
			for len(slice) > 0 {
				if r, size := utf8.DecodeLastRune(slice); r == utf8.RuneError && size == 1 {
					slice = slice[:len(slice)-1]
					continue
				}
				break
			}
		}
	}

	if textual {
		return offset, string(slice), "utf-8", truncated
	}
	return offset, base64.StdEncoding.EncodeToString(slice), "base64", truncated
}

func handleGetArticle(_ context.Context, _ *mcp.CallToolRequest, in getArticleIn) (*mcp.CallToolResult, getArticleOut, error) {
	e, errRes, errOut, ok := resolveArchive[getArticleOut](in.Archive)
	if !ok {
		return errRes, errOut, nil
	}

	entry, err := e.Archive.GetEntryByPath(in.Path)
	if err != nil {
		return toolErr[getArticleOut]("entry %q not found in %q: %v", in.Path, in.Archive, err)
	}
	if entry.IsRedirect() {
		entry, err = entry.ResolveRedirect(maxRedirectDepth)
		if err != nil {
			return toolErr[getArticleOut]("resolving redirect for %q: %v", in.Path, err)
		}
	}

	content, err := entry.Content()
	if err != nil {
		return toolErr[getArticleOut]("reading content of %q: %v", in.Path, err)
	}

	maxBytes := in.MaxBytes
	if in.MaxBytes == 0 {
		maxBytes = defaultMaxBytes
	}
	mime := entry.MimeType()
	offset, text, encoding, truncated := clipContent(content, in.Offset, maxBytes, isTextual(mime))

	return nil, getArticleOut{
		Archive:   in.Archive,
		Path:      entry.Path(),
		Title:     entry.Title(),
		MimeType:  mime,
		Content:   text,
		Encoding:  encoding,
		Offset:    offset,
		TotalSize: len(content),
		Truncated: truncated,
	}, nil
}

// ---- get_metadata ----

type archiveSelector struct {
	Archive string `json:"archive" jsonschema:"slug of the archive (from list_archives)"`
}

type getMetadataOut struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Language    string `json:"language,omitempty"`
	Creator     string `json:"creator,omitempty"`
	Date        string `json:"date,omitempty"`
	Description string `json:"description,omitempty"`
	EntryCount  uint32 `json:"entry_count"`
	UUID        string `json:"uuid"`
	Fulltext    bool   `json:"fulltext"`
}

func handleGetMetadata(_ context.Context, _ *mcp.CallToolRequest, in archiveSelector) (*mcp.CallToolResult, getMetadataOut, error) {
	e, errRes, errOut, ok := resolveArchive[getMetadataOut](in.Archive)
	if !ok {
		return errRes, errOut, nil
	}
	a := e.Archive
	uuid := a.UUID()
	return nil, getMetadataOut{
		Slug:        in.Archive,
		Title:       a.Title(),
		Language:    a.Language(),
		Creator:     a.Creator(),
		Date:        a.Date(),
		Description: a.Description(),
		EntryCount:  a.EntryCount(),
		UUID:        hex.EncodeToString(uuid[:]),
		Fulltext:    e.IndexPath != "",
	}, nil
}

// ---- main_page ----

type mainPageOut struct {
	Archive  string `json:"archive"`
	Path     string `json:"path"`
	Title    string `json:"title"`
	MimeType string `json:"mime_type"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"` // "utf-8" for text, "base64" for binary
}

func handleMainPage(_ context.Context, _ *mcp.CallToolRequest, in archiveSelector) (*mcp.CallToolResult, mainPageOut, error) {
	e, errRes, errOut, ok := resolveArchive[mainPageOut](in.Archive)
	if !ok {
		return errRes, errOut, nil
	}
	entry, err := e.Archive.MainEntry()
	if err != nil {
		return toolErr[mainPageOut]("no main page for %q: %v", in.Archive, err)
	}
	if entry.IsRedirect() {
		entry, err = entry.ResolveRedirect(maxRedirectDepth)
		if err != nil {
			return toolErr[mainPageOut]("resolving main-page redirect: %v", err)
		}
	}
	content, err := entry.Content()
	if err != nil {
		return toolErr[mainPageOut]("reading main page content: %v", err)
	}
	mime := entry.MimeType()
	_, text, encoding, _ := clipContent(content, 0, 0, isTextual(mime))
	return nil, mainPageOut{
		Archive:  in.Archive,
		Path:     entry.Path(),
		Title:    entry.Title(),
		MimeType: mime,
		Content:  text,
		Encoding: encoding,
	}, nil
}
