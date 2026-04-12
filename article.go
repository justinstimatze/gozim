package zim

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	RedirectEntry   uint16 = 0xffff
	LinkTargetEntry uint16 = 0xfffe
	DeletedEntry    uint16 = 0xfffd
)

type article struct {
	// EntryType is a RedirectEntry/LinkTargetEntry/DeletedEntry or an idx
	// pointing to zimReader.mimeTypeList
	EntryType uint16
	Title     string
	URLPtr    uint64
	Namespace byte
	url       string
	blob      uint32
	cluster   uint32
	z         *zimReader
}

// ArticleAtURLIdx returns the Article at URL index idx.
func (z *zimReader) ArticleAtURLIdx(idx uint32) (*article, error) {
	o, err := z.OffsetAtURLIdx(idx)
	if err != nil {
		return nil, err
	}
	return z.ArticleAt(o)
}

// MainPage returns the article designated as the main page, if any.
func (z *zimReader) MainPage() (*article, error) {
	if z.mainPage == 0xffffffff {
		return nil, nil
	}
	return z.ArticleAtURLIdx(z.mainPage)
}

// ArticleAt returns the Article at the given file offset using the article pool.
func (z *zimReader) ArticleAt(offset uint64) (*article, error) {
	a := z.articlePool.Get().(*article)
	err := z.FillArticleAt(a, offset)
	return a, err
}

// FillArticleAt fills an Article with data found at the given offset.
func (z *zimReader) FillArticleAt(a *article, offset uint64) error {
	a.z = z
	a.URLPtr = offset

	b, err := z.bytesRangeAt(offset, offset+2)
	if err != nil {
		return fmt.Errorf("can't read article %w", err)
	}
	mimeIdx := le16(b)
	a.EntryType = mimeIdx

	if mimeIdx == LinkTargetEntry || mimeIdx == DeletedEntry {
		return nil
	}

	s, err := z.bytesRangeAt(offset+3, offset+4)
	if err != nil {
		return err
	}
	a.Namespace = s[0]

	b, err = z.bytesRangeAt(offset+8, offset+12)
	if err != nil {
		return err
	}
	a.cluster = le32(b)

	b, err = z.bytesRangeAt(offset+12, offset+16)
	if err != nil {
		return err
	}
	a.blob = le32(b)

	// Redirect
	if mimeIdx == RedirectEntry {
		b, err := z.bytesRangeAt(offset+12, offset+12+2048)
		if err != nil {
			return nil
		}
		bbuf := bytes.NewBuffer(b)
		a.url, err = bbuf.ReadString('\x00')
		if err != nil {
			return err
		}
		a.url = strings.TrimRight(a.url, "\x00")

		a.Title, err = bbuf.ReadString('\x00')
		if err != nil {
			return err
		}
		a.Title = strings.TrimRight(a.Title, "\x00")
		return err
	}

	b, err = z.bytesRangeAt(offset+16, offset+16+2048)
	if err != nil {
		return nil
	}
	bbuf := bytes.NewBuffer(b)
	a.url, err = bbuf.ReadString('\x00')
	if err != nil {
		return err
	}

	a.url = strings.TrimRight(string(a.url), "\x00")

	title, err := bbuf.ReadString('\x00')
	if err != nil {
		return err
	}
	title = strings.TrimRight(string(title), "\x00")
	if len(title) != 0 {
		a.Title = title[0:1] + title[1:]
	}
	return nil
}

// Data returns the uncompressed data associated with this article.
func (a *article) data() ([]byte, error) {
	if a.EntryType == RedirectEntry || a.EntryType == LinkTargetEntry || a.EntryType == DeletedEntry {
		return nil, nil
	}
	start, end, err := a.z.clusterOffsetsAtIdx(a.cluster)
	if err != nil {
		return nil, err
	}
	s, err := a.z.bytesRangeAt(start, start+1)
	if err != nil {
		return nil, err
	}
	compression := uint8(s[0]) & 0x0f // low 4 bits = compression type
	extended := s[0]&0x10 != 0         // bit 4 = extended (8-byte) blob offsets

	switch {
	case compression == 4 || compression == 5:
		return a.readCompressed(start, end, compression, extended)
	case compression == 0 || compression == 1:
		return a.readUncompressed(start, extended)
	default:
		return nil, fmt.Errorf("unhandled compression type %d", compression)
	}
}

func (a *article) readCompressed(start, end uint64, compression uint8, extended bool) ([]byte, error) {
	blob, ok := a.z.blobCache.Get(a.cluster)
	if !ok {
		b, err := a.z.bytesRangeAt(start+1, end+1)
		if err != nil {
			return nil, err
		}
		bbuf := bytes.NewBuffer(b)

		var dec io.ReadCloser
		switch compression {
		case 5:
			dec, err = NewZstdReader(bbuf)
		case 4:
			dec, err = NewXZReader(bbuf)
		}
		if err != nil {
			return nil, err
		}
		defer dec.Close()

		b, err = io.ReadAll(dec)
		if err != nil {
			return nil, err
		}
		blob = make([]byte, len(b))
		copy(blob, b)
		a.z.blobCache.Add(a.cluster, blob)
	}

	bs, be := blobOffsets(blob, a.blob, extended)
	c := make([]byte, be-bs)
	copy(c, blob[bs:be])
	return c, nil
}

func (a *article) readUncompressed(start uint64, extended bool) ([]byte, error) {
	startPos := start + 1

	if extended {
		blobOffset := uint64(a.blob) * 8
		b, err := a.z.bytesRangeAt(startPos+blobOffset, startPos+blobOffset+8)
		if err != nil {
			return nil, err
		}
		bs := le64(b)
		b, err = a.z.bytesRangeAt(startPos+blobOffset+8, startPos+blobOffset+16)
		if err != nil {
			return nil, err
		}
		be := le64(b)
		return a.z.bytesRangeAt(startPos+bs, startPos+be)
	}

	blobOffset := uint64(a.blob) * 4
	b, err := a.z.bytesRangeAt(startPos+blobOffset, startPos+blobOffset+4)
	if err != nil {
		return nil, err
	}
	bs := uint64(le32(b))
	b, err = a.z.bytesRangeAt(startPos+blobOffset+4, startPos+blobOffset+8)
	if err != nil {
		return nil, err
	}
	be := uint64(le32(b))
	return a.z.bytesRangeAt(startPos+bs, startPos+be)
}

// blobOffsets extracts the start and end offsets for blob at blobIdx from decompressed cluster data.
func blobOffsets(data []byte, blobIdx uint32, extended bool) (start, end uint64) {
	if extended {
		off := uint64(blobIdx) * 8
		start = le64(data[off : off+8])
		end = le64(data[off+8 : off+16])
	} else {
		off := uint64(blobIdx) * 4
		start = uint64(le32(data[off : off+4]))
		end = uint64(le32(data[off+4 : off+8]))
	}
	return
}

// MimeType returns the MIME type string for this article.
func (a *article) MimeType() string {
	if a.EntryType == RedirectEntry || a.EntryType == LinkTargetEntry || a.EntryType == DeletedEntry {
		return ""
	}
	return a.z.mimeTypeList[a.EntryType]
}

// FullURL returns the URL prefixed by the namespace (e.g., "A/page").
func (a *article) FullURL() string {
	return string(a.Namespace) + "/" + a.url
}

func (a *article) String() string {
	return fmt.Sprintf("Mime: 0x%x URL: [%s], Title: [%s], Cluster: 0x%x Blob: 0x%x",
		a.EntryType, a.FullURL(), a.Title, a.cluster, a.blob)
}

// RedirectIndex returns the redirect target index for RedirectEntry type articles.
func (a *article) RedirectIndex() (uint32, error) {
	if a.EntryType != RedirectEntry {
		return 0, errors.New("not a redirect entry")
	}
	return a.cluster, nil
}
