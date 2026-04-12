package zim

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"

	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	zimMagic = 72173914
)

// ZimReader keeps track of everything related to ZIM reading.
type ZimReader struct {
	f             *os.File
	ArticleCount  uint32
	clusterCount  uint32
	urlPtrPos     uint64
	titlePtrPos   uint64
	clusterPtrPos uint64
	mimeListPos   uint64
	checksumPos   uint64
	mainPage      uint32
	layoutPage    uint32
	versionMajor  uint16
	versionMinor  uint16
	uuid          [16]byte
	mimeTypeList  []string
	mmap          []byte

	// Per-reader pool and cache (not global, avoids races between readers)
	articlePool sync.Pool
	blobCache   *lru.Cache[uint32, []byte]
}

func newBlobCache(size int) (*lru.Cache[uint32, []byte], error) {
	return lru.New[uint32, []byte](size)
}

// NewReader creates a new ZIM reader. If mmap is true, the file is memory-mapped.
func NewReader(path string, mmap bool) (*ZimReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	z := ZimReader{f: f, mainPage: 0xffffffff, layoutPage: 0xffffffff}

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := fi.Size()

	if mmap {
		pc := size / int64(os.Getpagesize())
		totalMmap := pc*int64(os.Getpagesize()) + int64(os.Getpagesize())
		if (size % int64(os.Getpagesize())) == 0 {
			totalMmap = size
		}

		mapped, err := syscall.Mmap(int(f.Fd()), 0, int(totalMmap), syscall.PROT_READ, syscall.MAP_PRIVATE)
		if err != nil {
			return nil, fmt.Errorf("can't mmap %w", err)
		}
		z.mmap = mapped
	}

	z.articlePool = sync.Pool{
		New: func() interface{} {
			return new(Article)
		},
	}
	z.blobCache, _ = lru.New[uint32, []byte](5)

	err = z.readFileHeaders()
	return &z, err
}

// MimeTypes returns an ordered list of MIME types present in the ZIM file.
func (z *ZimReader) MimeTypes() []string {
	if len(z.mimeTypeList) != 0 {
		return z.mimeTypeList
	}

	var s []string
	b, err := z.bytesRangeAt(z.mimeListPos, z.mimeListPos+2048)
	if err != nil {
		return s
	}
	bbuf := bytes.NewBuffer(b)

	for {
		line, err := bbuf.ReadBytes('\x00')
		if err != nil && err != io.EOF {
			return s
		}
		if len(line) == 1 {
			break
		}
		s = append(s, strings.TrimRight(string(line), "\x00"))
	}
	z.mimeTypeList = s
	return s
}

// ListArticles lists all articles via a channel. Deprecated: use Archive.Entries() instead.
func (z *ZimReader) ListArticles() <-chan *Article {
	ch := make(chan *Article, 10)

	go func() {
		var start uint32 = 1
		for idx := start; idx < z.ArticleCount; idx++ {
			art, err := z.ArticleAtURLIdx(idx)
			if err != nil {
				continue
			}
			ch <- art
		}
		close(ch)
	}()
	return ch
}

// ListTitlesPtr lists all title pointers via a channel. Deprecated: use Archive.EntriesByTitle() instead.
func (z *ZimReader) ListTitlesPtr() <-chan uint32 {
	ch := make(chan uint32, 10)

	go func() {
		var count uint32
		for pos := z.titlePtrPos; count < z.ArticleCount; pos += 4 {
			b, err := z.bytesRangeAt(pos, pos+4)
			if err != nil {
				count++
				continue
			}
			ch <- le32(b)
			count++
		}
		close(ch)
	}()
	return ch
}

// ListTitlesPtrIterator iterates all title pointers via a callback.
func (z *ZimReader) ListTitlesPtrIterator(cb func(uint32)) {
	var count uint32
	for pos := z.titlePtrPos; count < z.ArticleCount; pos += 4 {
		b, err := z.bytesRangeAt(pos, pos+4)
		if err != nil {
			count++
			continue
		}
		cb(le32(b))
		count++
	}
}

// GetPageNoIndex finds an article by its full URL using binary search on the URL index.
func (z *ZimReader) GetPageNoIndex(url string) (*Article, error) {
	var start uint32
	stop := z.ArticleCount

	a := new(Article)

	for {
		pos := (start + stop) / 2

		offset, err := z.OffsetAtURLIdx(pos)
		if err != nil {
			return nil, err
		}
		err = z.FillArticleAt(a, offset)
		if err != nil {
			return nil, err
		}

		if a.FullURL() == url {
			return a, nil
		}

		if a.FullURL() > url {
			stop = pos
		} else {
			start = pos
		}
		if stop-start == 1 {
			break
		}
	}
	return nil, errors.New("article not found")
}

// OffsetAtURLIdx returns the file offset for the entry at position idx in the URL index.
func (z *ZimReader) OffsetAtURLIdx(idx uint32) (uint64, error) {
	offset := z.urlPtrPos + uint64(idx)*8
	b, err := z.bytesRangeAt(offset, offset+8)
	if err != nil {
		return 0, err
	}
	return le64(b), nil
}

// Close releases all resources associated with the ZimReader.
func (z *ZimReader) Close() error {
	var mmapErr error
	if len(z.mmap) > 0 {
		mmapErr = syscall.Munmap(z.mmap)
		z.mmap = nil
	}
	fileErr := z.f.Close()
	return errors.Join(mmapErr, fileErr)
}

func (z *ZimReader) String() string {
	fi, err := z.f.Stat()
	if err != nil {
		return "corrupted zim"
	}
	return fmt.Sprintf("Size: %d, ArticleCount: %d urlPtrPos: 0x%x titlePtrPos: 0x%x mimeListPos: 0x%x clusterPtrPos: 0x%x\nMimeTypes: %v",
		fi.Size(), z.ArticleCount, z.urlPtrPos, z.titlePtrPos, z.mimeListPos, z.clusterPtrPos, z.MimeTypes())
}

// readTitleAt reads just namespace and title from the directory entry at urlIdx,
// without parsing cluster/blob data or decompressing content.
func (z *ZimReader) readTitleAt(urlIdx uint32) (namespace byte, title string, err error) {
	offset, err := z.OffsetAtURLIdx(urlIdx)
	if err != nil {
		return 0, "", err
	}

	// Read mime type (2 bytes) and namespace (byte at offset+3)
	b, err := z.bytesRangeAt(offset, offset+4)
	if err != nil {
		return 0, "", err
	}
	mimeIdx := le16(b[0:2])
	namespace = b[3]

	// Title starts at different offsets depending on entry type
	var titleStart uint64
	if mimeIdx == RedirectEntry {
		titleStart = offset + 12
	} else if mimeIdx == LinkTargetEntry || mimeIdx == DeletedEntry {
		return namespace, "", nil
	} else {
		titleStart = offset + 16
	}

	// Read url + title (null-terminated strings, assume < 2KB total)
	raw, err := z.bytesRangeAt(titleStart, titleStart+2048)
	if err != nil {
		return namespace, "", nil
	}

	// Skip past url (null-terminated)
	nullPos := 0
	for nullPos < len(raw) && raw[nullPos] != 0 {
		nullPos++
	}
	if nullPos >= len(raw) {
		return namespace, "", nil
	}

	// Read title (null-terminated after url)
	titleBytes := raw[nullPos+1:]
	titleEnd := 0
	for titleEnd < len(titleBytes) && titleBytes[titleEnd] != 0 {
		titleEnd++
	}

	title = string(titleBytes[:titleEnd])
	if title == "" {
		// Empty title means use path as title
		title = string(raw[:nullPos])
	}
	return namespace, title, nil
}

// bytesRangeAt returns bytes from start to end, using mmap if available.
func (z *ZimReader) bytesRangeAt(start, end uint64) ([]byte, error) {
	if len(z.mmap) > 0 {
		return z.mmap[start:end], nil
	}

	buf := make([]byte, end-start)
	n, err := z.f.ReadAt(buf, int64(start))
	if err != nil {
		return nil, fmt.Errorf("can't read bytes %w", err)
	}

	if n != int(end-start) {
		return nil, errors.New("can't read enough bytes")
	}

	return buf, nil
}

// readFileHeaders parses the ZIM file header (80 bytes).
func (z *ZimReader) readFileHeaders() error {
	// Read the full 80-byte header in one call
	hdr, err := z.bytesRangeAt(0, 80)
	if err != nil {
		return fmt.Errorf("can't read ZIM header: %w", err)
	}

	// Magic number
	if le32(hdr[0:4]) != zimMagic {
		return errors.New("not a ZIM file")
	}

	// Version (major at offset 4, minor at offset 6)
	z.versionMajor = le16(hdr[4:6])
	z.versionMinor = le16(hdr[6:8])
	if z.versionMajor != 5 && z.versionMajor != 6 {
		return fmt.Errorf("unsupported ZIM version %d.%d (only v5 and v6 supported)", z.versionMajor, z.versionMinor)
	}

	// UUID
	copy(z.uuid[:], hdr[8:24])

	// Entry and cluster counts
	z.ArticleCount = le32(hdr[24:28])
	z.clusterCount = le32(hdr[28:32])

	// Pointer positions
	z.urlPtrPos = le64(hdr[32:40])
	z.titlePtrPos = le64(hdr[40:48])
	z.clusterPtrPos = le64(hdr[48:56])
	z.mimeListPos = le64(hdr[56:64])

	// Main and layout pages
	z.mainPage = le32(hdr[64:68])
	z.layoutPage = le32(hdr[68:72])

	// Checksum position
	z.checksumPos = le64(hdr[72:80])

	z.MimeTypes()
	return nil
}

// clusterOffsetsAtIdx returns the start and end file offsets for the cluster at index idx.
func (z *ZimReader) clusterOffsetsAtIdx(idx uint32) (start, end uint64, err error) {
	offset := z.clusterPtrPos + (uint64(idx) * 8)
	b, err := z.bytesRangeAt(offset, offset+8)
	if err != nil {
		return
	}
	start = le64(b)

	if idx+1 < z.clusterCount {
		offset = z.clusterPtrPos + (uint64(idx+1) * 8)
		b, err = z.bytesRangeAt(offset, offset+8)
		if err != nil {
			return
		}
		end = le64(b) - 1
	} else {
		// Last cluster: end is the checksum position (or EOF)
		end = z.checksumPos - 1
	}
	return
}
