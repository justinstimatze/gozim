package zim

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

const (
	RedirectEntry   uint16 = 0xffff
	LinkTargetEntry uint16 = 0xfffe
	DeletedEntry    uint16 = 0xfffd
)

type article struct {
	entryType uint16
	title     string
	namespace byte
	url       string
	blob      uint32
	cluster   uint32
	z         *zimReader
}

func (a *article) fullURL() string {
	return string(a.namespace) + "/" + a.url
}

func (a *article) mimeType() string {
	if a.entryType == RedirectEntry || a.entryType == LinkTargetEntry || a.entryType == DeletedEntry {
		return ""
	}
	return a.z.mimeTypeList[a.entryType]
}

func (a *article) redirectIndex() (uint32, error) {
	if a.entryType != RedirectEntry {
		return 0, errors.New("not a redirect entry")
	}
	return a.cluster, nil
}

func (a *article) String() string {
	return fmt.Sprintf("Entry: [%s] Title: [%s] Cluster: 0x%x Blob: 0x%x",
		a.fullURL(), a.title, a.cluster, a.blob)
}

func (a *article) data() ([]byte, error) {
	if a.entryType == RedirectEntry || a.entryType == LinkTargetEntry || a.entryType == DeletedEntry {
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
	compression := s[0] & 0x0f
	extended := s[0]&0x10 != 0

	switch {
	case compression == 4 || compression == 5:
		return a.readCompressed(start, end, compression, extended)
	case compression == 0 || compression == 1:
		return a.readUncompressed(start, extended)
	default:
		return nil, fmt.Errorf("unhandled compression type %d", compression)
	}
}

func (a *article) readCompressed(start, end uint64, compression byte, extended bool) ([]byte, error) {
	blob, ok := a.z.blobCache.Get(a.cluster)
	if !ok {
		b, err := a.z.bytesRangeAt(start+1, end+1)
		if err != nil {
			return nil, err
		}

		var dec io.ReadCloser
		switch compression {
		case 5:
			dec, err = NewZstdReader(bytes.NewBuffer(b))
		case 4:
			dec, err = NewXZReader(bytes.NewBuffer(b))
		}
		if err != nil {
			return nil, err
		}
		defer dec.Close()

		raw, err := io.ReadAll(dec)
		if err != nil {
			return nil, err
		}
		blob = make([]byte, len(raw))
		copy(blob, raw)
		a.z.blobCache.Add(a.cluster, blob)
	}

	bs, be := blobOffsets(blob, a.blob, extended)
	out := make([]byte, be-bs)
	copy(out, blob[bs:be])
	return out, nil
}

func (a *article) readUncompressed(start uint64, extended bool) ([]byte, error) {
	pos := start + 1 // skip compression byte

	if extended {
		off := uint64(a.blob) * 8
		b, err := a.z.bytesRangeAt(pos+off, pos+off+16)
		if err != nil {
			return nil, err
		}
		bs, be := le64(b[0:8]), le64(b[8:16])
		return a.z.bytesRangeAt(pos+bs, pos+be)
	}

	off := uint64(a.blob) * 4
	b, err := a.z.bytesRangeAt(pos+off, pos+off+8)
	if err != nil {
		return nil, err
	}
	bs, be := uint64(le32(b[0:4])), uint64(le32(b[4:8]))
	return a.z.bytesRangeAt(pos+bs, pos+be)
}

func blobOffsets(data []byte, blobIdx uint32, extended bool) (start, end uint64) {
	if extended {
		off := uint64(blobIdx) * 8
		return le64(data[off : off+8]), le64(data[off+8 : off+16])
	}
	off := uint64(blobIdx) * 4
	return uint64(le32(data[off : off+4])), uint64(le32(data[off+4 : off+8]))
}
