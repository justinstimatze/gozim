package zim

import (
	"errors"
	"fmt"
)

// Entry represents a single entry in a ZIM archive.
type Entry struct {
	article *article
	archive *Archive
}

// Path returns the entry's path without the namespace prefix.
func (e Entry) Path() string { return e.article.url }

// Title returns the entry's title, falling back to Path() if empty.
func (e Entry) Title() string {
	if e.article.title != "" {
		return e.article.title
	}
	return e.article.url
}

// FullPath returns the full path including namespace prefix (e.g., "A/page").
func (e Entry) FullPath() string { return e.article.fullURL() }

// Content returns the decompressed content of this entry.
// Returns an error if the entry is a redirect (use RedirectTarget or ResolveRedirect instead).
func (e Entry) Content() ([]byte, error) {
	data, err := e.article.data()
	if errors.Is(err, errRedirectEntry) || errors.Is(err, errDeletedEntry) {
		return nil, err
	}
	return data, err
}

// MimeType returns the MIME type string for this entry.
func (e Entry) MimeType() string { return e.article.mimeType() }

// Namespace returns the namespace byte (e.g., 'A', 'C', 'M', '-').
func (e Entry) Namespace() byte { return e.article.namespace }

// IsRedirect returns true if this entry is a redirect to another entry.
func (e Entry) IsRedirect() bool { return e.article.entryType == RedirectEntry }

// IsArticle returns true if this entry is a front article (navigable content).
// For v5 ZIM files this is namespace 'A', for v6 namespace 'C'.
func (e Entry) IsArticle() bool {
	if e.IsRedirect() {
		return false
	}
	if e.archive.r.versionMajor >= 6 {
		return e.article.namespace == 'C'
	}
	return e.article.namespace == 'A'
}

// RedirectTarget follows one level of redirect and returns the target entry.
func (e Entry) RedirectTarget() (Entry, error) {
	idx, err := e.article.redirectIndex()
	if err != nil {
		return Entry{}, fmt.Errorf("not a redirect: %w", err)
	}
	return e.archive.GetEntryByIndex(idx)
}

// ResolveRedirect follows redirect chains up to maxDepth levels.
// Returns the final non-redirect entry, or an error if maxDepth is exceeded.
func (e Entry) ResolveRedirect(maxDepth int) (Entry, error) {
	cur := e
	for i := 0; i < maxDepth; i++ {
		if !cur.IsRedirect() {
			return cur, nil
		}
		next, err := cur.RedirectTarget()
		if err != nil {
			return Entry{}, err
		}
		cur = next
	}
	return Entry{}, fmt.Errorf("redirect chain exceeds max depth %d", maxDepth)
}

// EntryType returns the raw entry type value.
func (e Entry) EntryType() uint16 { return e.article.entryType }

func (e Entry) String() string { return e.article.String() }
