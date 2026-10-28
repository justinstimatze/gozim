package zim

import (
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

const defaultBlobCacheSize = 5

// Archive is the primary type for reading ZIM files.
type Archive struct {
	r      *zimReader
	meta   metadataCache
	search searchState

	listingOnce sync.Once
	listing     titleListing
}

type archiveConfig struct {
	mmap          bool
	blobCacheSize int
}

// Option configures Archive behavior.
type Option func(*archiveConfig)

// WithMmap enables memory-mapped file access.
func WithMmap() Option {
	return func(c *archiveConfig) { c.mmap = true }
}

// WithBlobCacheSize sets the number of decompressed clusters to cache (default 5).
func WithBlobCacheSize(n int) Option {
	return func(c *archiveConfig) { c.blobCacheSize = n }
}

// Open opens a ZIM file and returns an Archive.
func Open(path string, opts ...Option) (*Archive, error) {
	cfg := archiveConfig{blobCacheSize: defaultBlobCacheSize}
	for _, o := range opts {
		o(&cfg)
	}
	r, err := newReader(path, cfg.blobCacheSize, cfg.mmap)
	if err != nil {
		return nil, err
	}
	return &Archive{r: r}, nil
}

// EntryCount returns the number of entries in the archive.
func (a *Archive) EntryCount() uint32 { return a.r.articleCount }

// ClusterCount returns the number of clusters in the archive.
func (a *Archive) ClusterCount() uint32 { return a.r.clusterCount }

// UUID returns the unique identifier of the archive.
func (a *Archive) UUID() [16]byte { return a.r.uuid }

// VersionMajor returns the ZIM format major version (5 or 6).
func (a *Archive) VersionMajor() uint16 { return a.r.versionMajor }

// VersionMinor returns the ZIM format minor version.
func (a *Archive) VersionMinor() uint16 { return a.r.versionMinor }

// MimeTypes returns the ordered list of MIME types in the archive.
func (a *Archive) MimeTypes() []string { return a.r.MimeTypes() }

// articleNamespace returns the namespace used for articles in this ZIM version.
func (a *Archive) articleNamespace() byte {
	if a.r.versionMajor >= 6 {
		return 'C'
	}
	return 'A'
}

// GetEntryByPath finds an entry by its path (without namespace prefix).
// Searches the article namespace (A for v5, C for v6).
func (a *Archive) GetEntryByPath(path string) (Entry, error) {
	art, err := a.r.getArticle(string(a.articleNamespace()) + "/" + path)
	if err != nil {
		return Entry{}, err
	}
	return Entry{article: art, archive: a}, nil
}

// GetEntryByFullPath finds an entry by its full path including namespace prefix (e.g., "A/page").
func (a *Archive) GetEntryByFullPath(fullPath string) (Entry, error) {
	art, err := a.r.getArticle(fullPath)
	if err != nil {
		return Entry{}, err
	}
	return Entry{article: art, archive: a}, nil
}

// GetEntryByIndex returns the entry at the given URL index position.
func (a *Archive) GetEntryByIndex(idx uint32) (Entry, error) {
	art, err := a.r.articleAtIdx(idx)
	if err != nil {
		return Entry{}, err
	}
	return Entry{article: art, archive: a}, nil
}

// MainEntry returns the main page entry, if one is designated.
func (a *Archive) MainEntry() (Entry, error) {
	art, err := a.r.mainArticle()
	if err != nil {
		return Entry{}, err
	}
	if art == nil {
		return Entry{}, fmt.Errorf("no main page designated")
	}
	return Entry{article: art, archive: a}, nil
}

// SearchTitles searches for entries whose title starts with prefix, returning up to limit results.
// Searches article namespaces only (A for v5, C for v6). Uses binary search on the title index
// (v5) or the URL index (v6, which has no separate title list).
func (a *Archive) SearchTitles(prefix string, limit int) ([]Entry, error) {
	if limit <= 0 {
		return nil, nil
	}

	ns := a.articleNamespace()

	// v6 ZIM files set titlePtrPos to 0xFFFFFFFFFFFFFFFF (no title list).
	// They normally ship a title-ordered listing instead; without one, fall
	// back to scanning the URL index within the article namespace.
	if a.r.titlePtrPos == 0xFFFFFFFFFFFFFFFF {
		if l := a.titleListing(); l.count > 0 {
			return a.searchTitlesByListing(l, ns, prefix, limit)
		}
		return a.searchTitlesByURL(ns, prefix, limit)
	}

	return a.searchTitlesByTitleIdx(ns, prefix, limit)
}

// searchTitlesByTitleIdx uses the v5 title pointer list for prefix search.
func (a *Archive) searchTitlesByTitleIdx(ns byte, prefix string, limit int) ([]Entry, error) {
	fullPrefix := string(ns) + prefix

	lo, hi := uint32(0), a.r.articleCount
	for lo < hi {
		mid := lo + (hi-lo)/2
		mns, title, err := a.readTitleAtTitleIdx(mid)
		if err != nil {
			return nil, err
		}
		if string(mns)+title < fullPrefix {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	var results []Entry
	for i := lo; i < a.r.articleCount && len(results) < limit; i++ {
		mns, title, err := a.readTitleAtTitleIdx(i)
		if err != nil {
			continue
		}
		if mns != ns {
			break
		}
		if len(title) < len(prefix) || title[:len(prefix)] != prefix {
			break
		}
		urlIdx, err := a.titleIdxToURLIdx(i)
		if err != nil {
			continue
		}
		art, err := a.r.articleAtIdx(urlIdx)
		if err != nil {
			continue
		}
		results = append(results, Entry{article: art, archive: a})
	}
	return results, nil
}

// searchTitlesByURL scans the URL index for v6 ZIM files that lack a title pointer list.
// Uses lowerBound to find the namespace start, then linear scans for title prefix matches.
func (a *Archive) searchTitlesByURL(ns byte, prefix string, limit int) ([]Entry, error) {
	lo, err := a.lowerBound(string(ns) + "/")
	if err != nil {
		return nil, err
	}

	var results []Entry
	for idx := lo; idx < a.r.articleCount && len(results) < limit; idx++ {
		art, err := a.r.articleAtIdx(idx)
		if err != nil {
			continue
		}
		if art.namespace != ns {
			break
		}
		title := art.title
		if title == "" {
			title = art.url
		}
		if len(title) >= len(prefix) && title[:len(prefix)] == prefix {
			results = append(results, Entry{article: art, archive: a})
		}
	}
	return results, nil
}

func (a *Archive) readTitleAtTitleIdx(i uint32) (namespace byte, title string, err error) {
	urlIdx, err := a.titleIdxToURLIdx(i)
	if err != nil {
		return 0, "", err
	}
	return a.r.readTitleAt(urlIdx)
}

func (a *Archive) titleIdxToURLIdx(i uint32) (uint32, error) {
	pos := a.r.titlePtrPos + uint64(i)*4
	b, err := a.r.bytesRangeAt(pos, pos+4)
	if err != nil {
		return 0, err
	}
	return le32(b), nil
}

// ValidateChecksum verifies the MD5 integrity checksum stored at the end of the ZIM file.
func (a *Archive) ValidateChecksum() (bool, error) {
	if a.r.checksumPos == 0 {
		return false, fmt.Errorf("no checksum in header")
	}

	stored, err := a.r.bytesRangeAt(a.r.checksumPos, a.r.checksumPos+16)
	if err != nil {
		return false, fmt.Errorf("can't read stored checksum: %w", err)
	}

	h := md5.New()
	if len(a.r.mmap) > 0 {
		h.Write(a.r.mmap[:a.r.checksumPos])
	} else {
		// Open a separate file descriptor for streaming to avoid race with ReadAt.
		f, err := os.Open(a.r.f.Name())
		if err != nil {
			return false, fmt.Errorf("can't reopen for checksum: %w", err)
		}
		defer f.Close()
		if _, err := io.CopyN(h, f, int64(a.r.checksumPos)); err != nil {
			return false, fmt.Errorf("can't compute checksum: %w", err)
		}
	}

	computed := h.Sum(nil)
	return string(computed) == string(stored), nil
}

// Close releases all resources associated with the archive. It must not be
// called while other methods on the archive or its entries are still running;
// with WithMmap, a read that overlaps Close can touch unmapped memory.
func (a *Archive) Close() error {
	return errors.Join(a.closeSearch(), a.r.Close())
}

func (a *Archive) String() string { return a.r.String() }

// titleListing locates X/listing/titleOrdered/v1, the uint32 URL indexes of a
// v6 archive's front articles sorted by title. It is read in place from the
// file, so it must sit in an uncompressed cluster; count is 0 when absent.
type titleListing struct {
	off   uint64
	count uint32
}

func (a *Archive) titleListing() titleListing {
	a.listingOnce.Do(func() {
		e, err := a.GetEntryByFullPath("X/listing/titleOrdered/v1")
		if err != nil {
			return
		}
		start, end, err := e.article.uncompressedRange()
		if err != nil || (end-start)%4 != 0 || (end-start)/4 > uint64(a.r.articleCount) {
			return
		}
		a.listing = titleListing{off: start, count: uint32((end - start) / 4)}
	})
	return a.listing
}

// listingURLIdx returns the URL index stored at position i of the listing.
func (a *Archive) listingURLIdx(l titleListing, i uint32) (uint32, error) {
	off := l.off + uint64(i)*4
	b, err := a.r.bytesRangeAt(off, off+4)
	if err != nil {
		return 0, err
	}
	idx := le32(b)
	if idx >= a.r.articleCount {
		return 0, fmt.Errorf("title listing entry %d points at entry %d of %d", i, idx, a.r.articleCount)
	}
	return idx, nil
}

// searchTitlesByListing binary-searches the title listing for prefix, so a
// lookup costs about log2(count) title reads instead of a namespace scan.
func (a *Archive) searchTitlesByListing(l titleListing, ns byte, prefix string, limit int) ([]Entry, error) {
	titleAt := func(i uint32) (uint32, string, error) {
		idx, err := a.listingURLIdx(l, i)
		if err != nil {
			return 0, "", err
		}
		_, title, err := a.r.readTitleAt(idx)
		return idx, title, err
	}

	lo, hi := uint32(0), l.count
	for lo < hi {
		mid := lo + (hi-lo)/2
		_, title, err := titleAt(mid)
		if err != nil {
			return nil, err
		}
		if title < prefix {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	var results []Entry
	for i := lo; i < l.count && len(results) < limit; i++ {
		idx, title, err := titleAt(i)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(title, prefix) {
			break
		}
		art, err := a.r.articleAtIdx(idx)
		if err != nil {
			return nil, err
		}
		if art.namespace == ns {
			results = append(results, Entry{article: art, archive: a})
		}
	}
	return results, nil
}
