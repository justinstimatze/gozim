package zim

import (
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
)

const defaultBlobCacheSize = 5

// Archive is the primary type for reading ZIM files.
type Archive struct {
	r      *zimReader
	meta   metadataCache
	search searchState
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
// Searches article namespaces only (A for v5, C for v6). Uses binary search on the title index.
func (a *Archive) SearchTitles(prefix string, limit int) ([]Entry, error) {
	if limit <= 0 {
		return nil, nil
	}

	ns := a.articleNamespace()
	fullPrefix := string(ns) + prefix

	lo, hi := uint32(0), a.r.articleCount
	for lo < hi {
		mid := lo + (hi-lo)/2
		ns, title, err := a.readTitleAtTitleIdx(mid)
		if err != nil {
			return nil, err
		}
		if string(ns)+title < fullPrefix {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	var results []Entry
	articleNS := a.articleNamespace()
	for i := lo; i < a.r.articleCount && len(results) < limit; i++ {
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
		art, err := a.r.articleAtIdx(urlIdx)
		if err != nil {
			continue
		}
		results = append(results, Entry{article: art, archive: a})
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

// Close releases all resources associated with the archive.
func (a *Archive) Close() error {
	return errors.Join(a.closeSearch(), a.r.Close())
}

func (a *Archive) String() string { return a.r.String() }
