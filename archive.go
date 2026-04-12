package zim

import (
	"crypto/md5"
	"fmt"
	"io"
)

// Archive is the primary type for reading ZIM files.
type Archive struct {
	r    *ZimReader
	meta metadataCache
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
	cfg := archiveConfig{blobCacheSize: 5}
	for _, o := range opts {
		o(&cfg)
	}

	r, err := NewReader(path, cfg.mmap)
	if err != nil {
		return nil, err
	}

	if cfg.blobCacheSize != 5 {
		cache, err := newBlobCache(cfg.blobCacheSize)
		if err != nil {
			return nil, fmt.Errorf("invalid cache size: %w", err)
		}
		r.blobCache = cache
	}

	return &Archive{r: r}, nil
}

// EntryCount returns the number of entries in the archive.
func (a *Archive) EntryCount() uint32 { return a.r.ArticleCount }

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

// GetEntryByPath finds an entry by its path (without namespace prefix).
// It searches namespace A (v5) or C (v6) first, then falls back to a full URL search.
func (a *Archive) GetEntryByPath(path string) (Entry, error) {
	ns := "A"
	if a.r.versionMajor >= 6 {
		ns = "C"
	}
	art, err := a.r.GetPageNoIndex(ns + "/" + path)
	if err != nil {
		return Entry{}, err
	}
	return Entry{article: art, archive: a}, nil
}

// GetEntryByFullPath finds an entry by its full path including namespace prefix (e.g., "A/page").
func (a *Archive) GetEntryByFullPath(fullPath string) (Entry, error) {
	art, err := a.r.GetPageNoIndex(fullPath)
	if err != nil {
		return Entry{}, err
	}
	return Entry{article: art, archive: a}, nil
}

// GetEntryByIndex returns the entry at the given URL index position.
func (a *Archive) GetEntryByIndex(idx uint32) (Entry, error) {
	art, err := a.r.ArticleAtURLIdx(idx)
	if err != nil {
		return Entry{}, err
	}
	return Entry{article: art, archive: a}, nil
}

// MainEntry returns the main page entry, if one is designated.
func (a *Archive) MainEntry() (Entry, error) {
	art, err := a.r.MainPage()
	if err != nil {
		return Entry{}, err
	}
	if art == nil {
		return Entry{}, fmt.Errorf("no main page designated")
	}
	return Entry{article: art, archive: a}, nil
}

// SearchTitles searches for entries whose title starts with prefix, returning up to limit results.
// Searches article namespaces only (A for v5, C for v6). Uses binary search on the title index.
func (a *Archive) SearchTitles(prefix string, limit int) ([]Entry, error) {
	if limit <= 0 {
		return nil, nil
	}

	articleNS := byte('A')
	if a.r.versionMajor >= 6 {
		articleNS = 'C'
	}

	// The title index is sorted by <namespace><title>.
	// We search for the article namespace + prefix.
	fullPrefix := string(articleNS) + prefix

	// Binary search on the title pointer list
	lo, hi := uint32(0), a.r.ArticleCount
	for lo < hi {
		mid := lo + (hi-lo)/2
		ns, title, err := a.readTitleAtTitleIdx(mid)
		if err != nil {
			return nil, err
		}
		key := string(ns) + title
		if key < fullPrefix {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	// Scan forward collecting matches
	var results []Entry
	for i := lo; i < a.r.ArticleCount && len(results) < limit; i++ {
		ns, title, err := a.readTitleAtTitleIdx(i)
		if err != nil {
			continue
		}
		if ns != articleNS {
			break
		}
		if len(title) < len(prefix) || title[:len(prefix)] != prefix {
			break
		}

		urlIdx, err := a.titleIdxToURLIdx(i)
		if err != nil {
			continue
		}
		art, err := a.r.ArticleAtURLIdx(urlIdx)
		if err != nil {
			continue
		}
		results = append(results, Entry{article: art, archive: a})
	}
	return results, nil
}

// readTitleAtTitleIdx reads the namespace and title for the entry at title index position i,
// without decompressing any cluster data.
func (a *Archive) readTitleAtTitleIdx(i uint32) (namespace byte, title string, err error) {
	urlIdx, err := a.titleIdxToURLIdx(i)
	if err != nil {
		return 0, "", err
	}
	return a.r.readTitleAt(urlIdx)
}

// titleIdxToURLIdx reads the URL index value from the title pointer list at position i.
func (a *Archive) titleIdxToURLIdx(i uint32) (uint32, error) {
	pos := a.r.titlePtrPos + uint64(i)*4
	b, err := a.r.bytesRangeAt(pos, pos+4)
	if err != nil {
		return 0, err
	}
	return le32(b), nil
}

// ValidateChecksum verifies the MD5 checksum stored at the end of the ZIM file.
// Returns true if the checksum matches, false otherwise.
func (a *Archive) ValidateChecksum() (bool, error) {
	if a.r.checksumPos == 0 {
		return false, fmt.Errorf("no checksum position in header")
	}

	// Read stored checksum (last 16 bytes at checksumPos)
	stored, err := a.r.bytesRangeAt(a.r.checksumPos, a.r.checksumPos+16)
	if err != nil {
		return false, fmt.Errorf("can't read stored checksum: %w", err)
	}

	// Compute MD5 of file[0:checksumPos]
	h := md5.New()
	if len(a.r.mmap) > 0 {
		h.Write(a.r.mmap[:a.r.checksumPos])
	} else {
		// Stream from file
		if _, err := a.r.f.Seek(0, 0); err != nil {
			return false, err
		}
		if _, err := io.CopyN(h, a.r.f, int64(a.r.checksumPos)); err != nil {
			return false, fmt.Errorf("can't compute checksum: %w", err)
		}
	}

	computed := h.Sum(nil)
	return string(computed) == string(stored), nil
}

// Close releases all resources associated with the archive.
func (a *Archive) Close() error {
	return a.r.Close()
}

func (a *Archive) String() string {
	return a.r.String()
}
