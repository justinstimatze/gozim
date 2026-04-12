package zim

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	lru "github.com/hashicorp/golang-lru/v2"
)

const zimMagic = 72173914

type zimReader struct {
	f             *os.File
	articleCount  uint32
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
	blobCache     *lru.Cache[uint32, []byte]
}

func newReader(path string, cacheSize int, mmap bool) (*zimReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	z := zimReader{f: f, mainPage: 0xffffffff, layoutPage: 0xffffffff}

	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	if mmap {
		size := fi.Size()
		totalMmap := ((size / int64(os.Getpagesize())) + 1) * int64(os.Getpagesize())
		if size%int64(os.Getpagesize()) == 0 {
			totalMmap = size
		}
		mapped, err := syscall.Mmap(int(f.Fd()), 0, int(totalMmap), syscall.PROT_READ, syscall.MAP_PRIVATE)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("can't mmap: %w", err)
		}
		z.mmap = mapped
	}

	z.blobCache, _ = lru.New[uint32, []byte](cacheSize)

	if err := z.readFileHeaders(); err != nil {
		z.Close()
		return nil, err
	}
	return &z, nil
}

// MimeTypes returns an ordered list of MIME types present in the ZIM file.
func (z *zimReader) MimeTypes() []string {
	if len(z.mimeTypeList) != 0 {
		return z.mimeTypeList
	}

	b, err := z.bytesRangeAt(z.mimeListPos, z.mimeListPos+2048)
	if err != nil {
		return nil
	}
	bbuf := bytes.NewBuffer(b)
	var s []string
	for {
		line, err := bbuf.ReadBytes('\x00')
		if err != nil && err != io.EOF {
			break
		}
		if len(line) == 1 {
			break
		}
		s = append(s, strings.TrimRight(string(line), "\x00"))
	}
	z.mimeTypeList = s
	return s
}

func (z *zimReader) getArticle(url string) (*article, error) {
	var lo uint32
	hi := z.articleCount
	a := new(article)
	for lo < hi {
		mid := lo + (hi-lo)/2
		offset, err := z.OffsetAtURLIdx(mid)
		if err != nil {
			return nil, err
		}
		if err := z.fillArticle(a, offset); err != nil {
			return nil, err
		}
		full := a.fullURL()
		if full == url {
			return a, nil
		}
		if full > url {
			hi = mid
		} else {
			lo = mid + 1
		}
		if hi-lo == 0 {
			break
		}
	}
	return nil, errors.New("entry not found")
}

func (z *zimReader) articleAtIdx(idx uint32) (*article, error) {
	offset, err := z.OffsetAtURLIdx(idx)
	if err != nil {
		return nil, err
	}
	a := new(article)
	return a, z.fillArticle(a, offset)
}

func (z *zimReader) mainArticle() (*article, error) {
	if z.mainPage == 0xffffffff {
		return nil, nil
	}
	return z.articleAtIdx(z.mainPage)
}

// OffsetAtURLIdx returns the file offset for the entry at position idx in the URL index.
func (z *zimReader) OffsetAtURLIdx(idx uint32) (uint64, error) {
	offset := z.urlPtrPos + uint64(idx)*8
	b, err := z.bytesRangeAt(offset, offset+8)
	if err != nil {
		return 0, err
	}
	return le64(b), nil
}

// fillArticle populates an article from the directory entry at offset.
func (z *zimReader) fillArticle(a *article, offset uint64) error {
	*a = article{z: z} // zero all fields

	// Read first 16 bytes: mimeType(2) + paramLen(1) + namespace(1) + revision(4) + cluster/redirect(4) + blob(4)
	hdr, err := z.bytesRangeAt(offset, offset+16)
	if err != nil {
		return fmt.Errorf("can't read entry header: %w", err)
	}

	a.entryType = le16(hdr[0:2])

	if a.entryType == LinkTargetEntry || a.entryType == DeletedEntry {
		return nil
	}

	a.namespace = hdr[3]
	a.cluster = le32(hdr[8:12])
	a.blob = le32(hdr[12:16])

	if a.entryType == RedirectEntry {
		// For redirects: url + title start at offset+12 (after redirect index)
		return z.readURLTitle(a, offset+12)
	}

	// For content entries: url + title start at offset+16
	return z.readURLTitle(a, offset+16)
}

// readURLTitle reads null-terminated url and title strings starting at pos.
func (z *zimReader) readURLTitle(a *article, pos uint64) error {
	// Try 2048 bytes; fall back to whatever remains if near EOF.
	end := pos + 2048
	b, err := z.bytesRangeAt(pos, end)
	if err != nil {
		// Retry with file size as end bound
		fi, statErr := z.f.Stat()
		if statErr != nil {
			return nil
		}
		end = uint64(fi.Size())
		if end <= pos {
			return nil
		}
		b, err = z.bytesRangeAt(pos, end)
		if err != nil {
			return nil
		}
	}

	// url\0title\0
	urlEnd := bytes.IndexByte(b, 0)
	if urlEnd < 0 {
		return nil
	}
	a.url = string(b[:urlEnd])

	rest := b[urlEnd+1:]
	titleEnd := bytes.IndexByte(rest, 0)
	if titleEnd < 0 {
		return nil
	}
	if titleEnd > 0 {
		a.title = string(rest[:titleEnd])
	}
	return nil
}

// readTitleAt reads just namespace and title from the directory entry at urlIdx,
// without decompressing any cluster data.
func (z *zimReader) readTitleAt(urlIdx uint32) (namespace byte, title string, err error) {
	offset, err := z.OffsetAtURLIdx(urlIdx)
	if err != nil {
		return 0, "", err
	}

	hdr, err := z.bytesRangeAt(offset, offset+4)
	if err != nil {
		return 0, "", err
	}
	mimeIdx := le16(hdr[0:2])
	namespace = hdr[3]

	if mimeIdx == LinkTargetEntry || mimeIdx == DeletedEntry {
		return namespace, "", nil
	}

	var strStart uint64
	if mimeIdx == RedirectEntry {
		strStart = offset + 12
	} else {
		strStart = offset + 16
	}

	raw, err := z.bytesRangeAt(strStart, strStart+2048)
	if err != nil {
		return namespace, "", nil
	}

	urlEnd := bytes.IndexByte(raw, 0)
	if urlEnd < 0 {
		return namespace, "", nil
	}

	rest := raw[urlEnd+1:]
	titleEnd := bytes.IndexByte(rest, 0)
	if titleEnd > 0 {
		title = string(rest[:titleEnd])
	} else {
		title = string(raw[:urlEnd]) // empty title → use path
	}
	return namespace, title, nil
}

func (z *zimReader) Close() error {
	var mmapErr error
	if len(z.mmap) > 0 {
		mmapErr = syscall.Munmap(z.mmap)
		z.mmap = nil
	}
	fileErr := z.f.Close()
	return errors.Join(mmapErr, fileErr)
}

func (z *zimReader) String() string {
	fi, err := z.f.Stat()
	if err != nil {
		return "corrupted zim"
	}
	return fmt.Sprintf("Size: %d, Entries: %d, Clusters: %d, Version: %d.%d",
		fi.Size(), z.articleCount, z.clusterCount, z.versionMajor, z.versionMinor)
}

func (z *zimReader) bytesRangeAt(start, end uint64) ([]byte, error) {
	if len(z.mmap) > 0 {
		if end > uint64(len(z.mmap)) {
			return nil, fmt.Errorf("read beyond file: offset %d > size %d", end, len(z.mmap))
		}
		return z.mmap[start:end], nil
	}
	buf := make([]byte, end-start)
	n, err := z.f.ReadAt(buf, int64(start))
	if err != nil {
		return nil, fmt.Errorf("can't read %d bytes at offset %d: %w", end-start, start, err)
	}
	if n != int(end-start) {
		return nil, fmt.Errorf("short read: got %d, want %d", n, end-start)
	}
	return buf, nil
}

func (z *zimReader) readFileHeaders() error {
	hdr, err := z.bytesRangeAt(0, 80)
	if err != nil {
		return fmt.Errorf("can't read ZIM header: %w", err)
	}

	if le32(hdr[0:4]) != zimMagic {
		return errors.New("not a ZIM file")
	}

	z.versionMajor = le16(hdr[4:6])
	z.versionMinor = le16(hdr[6:8])
	if z.versionMajor != 5 && z.versionMajor != 6 {
		return fmt.Errorf("unsupported ZIM version %d.%d", z.versionMajor, z.versionMinor)
	}

	copy(z.uuid[:], hdr[8:24])
	z.articleCount = le32(hdr[24:28])
	z.clusterCount = le32(hdr[28:32])
	z.urlPtrPos = le64(hdr[32:40])
	z.titlePtrPos = le64(hdr[40:48])
	z.clusterPtrPos = le64(hdr[48:56])
	z.mimeListPos = le64(hdr[56:64])
	z.mainPage = le32(hdr[64:68])
	z.layoutPage = le32(hdr[68:72])
	z.checksumPos = le64(hdr[72:80])

	z.MimeTypes()
	return nil
}

func (z *zimReader) clusterOffsetsAtIdx(idx uint32) (start, end uint64, err error) {
	offset := z.clusterPtrPos + uint64(idx)*8
	b, err := z.bytesRangeAt(offset, offset+8)
	if err != nil {
		return 0, 0, err
	}
	start = le64(b)

	if idx+1 < z.clusterCount {
		b, err = z.bytesRangeAt(offset+8, offset+16)
		if err != nil {
			return 0, 0, err
		}
		end = le64(b) - 1
	} else {
		end = z.checksumPos - 1
	}
	return start, end, nil
}
