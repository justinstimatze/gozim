package zim

import "fmt"

// Entry represents a single entry in a ZIM archive.
type Entry struct {
	article *Article
	archive *Archive
}

// Path returns the entry's path without the namespace prefix.
func (e Entry) Path() string {
	return e.article.url
}

// Title returns the entry's title, falling back to Path() if the title is empty.
func (e Entry) Title() string {
	if e.article.Title != "" {
		return e.article.Title
	}
	return e.article.url
}

// FullPath returns the full path including namespace prefix (e.g., "A/page").
func (e Entry) FullPath() string {
	return e.article.FullURL()
}

// Content returns the decompressed content of this entry.
func (e Entry) Content() ([]byte, error) {
	return e.article.Data()
}

// MimeType returns the MIME type string for this entry.
func (e Entry) MimeType() string {
	return e.article.MimeType()
}

// Namespace returns the namespace byte (e.g., 'A', 'C', 'M', '-').
func (e Entry) Namespace() byte {
	return e.article.Namespace
}

// IsRedirect returns true if this entry is a redirect to another entry.
func (e Entry) IsRedirect() bool {
	return e.article.EntryType == RedirectEntry
}

// IsArticle returns true if this entry is a front article (navigable content).
// For v5 ZIM files, this is namespace 'A'. For v6, namespace 'C'.
func (e Entry) IsArticle() bool {
	if e.IsRedirect() {
		return false
	}
	if e.archive.r.versionMajor >= 6 {
		return e.article.Namespace == 'C'
	}
	return e.article.Namespace == 'A'
}

// RedirectTarget follows one level of redirect and returns the target entry.
func (e Entry) RedirectTarget() (Entry, error) {
	idx, err := e.article.RedirectIndex()
	if err != nil {
		return Entry{}, fmt.Errorf("not a redirect: %w", err)
	}
	return e.archive.GetEntryByIndex(idx)
}

// EntryType returns the raw entry type value.
func (e Entry) EntryType() uint16 {
	return e.article.EntryType
}

func (e Entry) String() string {
	return e.article.String()
}
