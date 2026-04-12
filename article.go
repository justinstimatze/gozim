package zim

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

const (
	RedirectEntry   uint16 = 0xffff
	LinkTargetEntry uint16 = 0xfffe
	DeletedEntry    uint16 = 0xfffd
)

// maxDecompressedSize limits decompressed cluster size to 256MB to prevent zip bombs.
const maxDecompressedSize = 256 << 20

var (
	errRedirectEntry = errors.New("entry is a redirect; call RedirectTarget() to follow")
	errDeletedEntry  = errors.New("entry is deleted")
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
	idx := int(a.entryType)
	if idx >= len(a.z.mimeTypeList) {
		return ""
	}
	return a.z.mimeTypeList[idx]
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
	if a.entryType == RedirectEntry {
		return nil, errRedirectEntry
	}
	if a.entryType == LinkTargetEntry || a.entryType == DeletedEntry {
		return nil, errDeletedEntry
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
		// Use singleflight to deduplicate concurrent decompressions of the same cluster.
		key := strconv.FormatUint(uint64(a.cluster), 10)
		v, err, _ := a.z.decompGroup.Do(key, func() (interface{}, error) {
			// Double-check cache after acquiring the flight.
			if cached, ok := a.z.blobCache.Get(a.cluster); ok {
				return cached, nil
			}
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
			raw, err := io.ReadAll(io.LimitReader(dec, maxDecompressedSize))
			if err != nil {
				return nil, err
			}
			result := make([]byte, len(raw))
			copy(result, raw)
			a.z.blobCache.Add(a.cluster, result)
			return result, nil
		})
		if err != nil {
			return nil, err
		}
		blob = v.([]byte)
	}

	bs, be, err := safeBlobOffsets(blob, a.blob, extended)
	if err != nil {
		return nil, err
	}
	out := make([]byte, be-bs)
	copy(out, blob[bs:be])
	return out, nil
}

func (a *article) readUncompressed(start uint64, extended bool) ([]byte, error) {
	pos := start + 1

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

// safeBlobOffsets extracts blob offsets with bounds checking.
func safeBlobOffsets(data []byte, blobIdx uint32, extended bool) (start, end uint64, err error) {
	if extended {
		off := uint64(blobIdx) * 8
		if off+16 > uint64(len(data)) {
			return 0, 0, fmt.Errorf("blob index %d out of range (cluster size %d)", blobIdx, len(data))
		}
		return le64(data[off : off+8]), le64(data[off+8 : off+16]), nil
	}
	off := uint64(blobIdx) * 4
	if off+8 > uint64(len(data)) {
		return 0, 0, fmt.Errorf("blob index %d out of range (cluster size %d)", blobIdx, len(data))
	}
	return uint64(le32(data[off : off+4])), uint64(le32(data[off+4 : off+8])), nil
}

// blobOffsets extracts blob offsets without bounds checking (for benchmarks/tests with known-good data).
func blobOffsets(data []byte, blobIdx uint32, extended bool) (start, end uint64) {
	if extended {
		off := uint64(blobIdx) * 8
		return le64(data[off : off+8]), le64(data[off+8 : off+16])
	}
	off := uint64(blobIdx) * 4
	return uint64(le32(data[off : off+4])), uint64(le32(data[off+4 : off+8]))
}
