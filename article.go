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

// maxDecompressedSize limits decompressed cluster size to 256MB to prevent zip
// bombs. A var only so tests can lower it.
var maxDecompressedSize = 256 << 20

// maxCachedClusterSize is the largest decompressed cluster kept in blobCache.
// Kiwix writes clusters of about 2 MiB, well under it. The limit stops a
// crafted archive from pinning cacheSize clusters of maxDecompressedSize each.
const maxCachedClusterSize = 16 << 20

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
				return nil, fmt.Errorf("cluster uses LZMA/XZ compression (type 4) which is not supported; this ZIM file was created before ~2020 — use a newer ZIM file with Zstandard compression from download.kiwix.org/zim/")
			}
			if err != nil {
				return nil, err
			}
			defer dec.Close()
			// Read one byte past the limit so an oversized cluster is an error
			// rather than silently truncated content.
			raw, err := io.ReadAll(io.LimitReader(dec, int64(maxDecompressedSize)+1))
			if err != nil {
				return nil, err
			}
			if len(raw) > maxDecompressedSize {
				return nil, fmt.Errorf("cluster %d decompresses to more than %d bytes", a.cluster, maxDecompressedSize)
			}
			result := make([]byte, len(raw))
			copy(result, raw)
			if len(result) <= maxCachedClusterSize {
				a.z.blobCache.Add(a.cluster, result)
			}
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
	bs, be, err := a.uncompressedBlobRange(start, extended)
	if err != nil {
		return nil, err
	}
	b, err := a.z.bytesRangeAt(bs, be)
	if err != nil || len(a.z.mmap) == 0 {
		return b, err
	}
	// Under mmap, b aliases the mapping that Close unmaps. Callers may keep
	// content past Close, so hand them a copy.
	return bytes.Clone(b), nil
}

// safeBlobOffsets extracts blob offsets with bounds checking, and verifies the
// blob range lies inside data.
func safeBlobOffsets(data []byte, blobIdx uint32, extended bool) (start, end uint64, err error) {
	if extended {
		off := uint64(blobIdx) * 8
		if off+16 > uint64(len(data)) {
			return 0, 0, fmt.Errorf("blob index %d out of range (cluster size %d)", blobIdx, len(data))
		}
		start, end = le64(data[off:off+8]), le64(data[off+8:off+16])
	} else {
		off := uint64(blobIdx) * 4
		if off+8 > uint64(len(data)) {
			return 0, 0, fmt.Errorf("blob index %d out of range (cluster size %d)", blobIdx, len(data))
		}
		start, end = uint64(le32(data[off:off+4])), uint64(le32(data[off+4:off+8]))
	}
	if start > end || end > uint64(len(data)) {
		return 0, 0, fmt.Errorf("blob %d has invalid range [%d, %d) (cluster size %d)", blobIdx, start, end, len(data))
	}
	return start, end, nil
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

// uncompressedBlobRange returns the file range [start, end) of this entry's
// blob in the uncompressed cluster that begins at clusterStart.
func (a *article) uncompressedBlobRange(clusterStart uint64, extended bool) (uint64, uint64, error) {
	pos := clusterStart + 1

	var bs, be uint64
	if extended {
		off := uint64(a.blob) * 8
		b, err := a.z.bytesRangeAt(pos+off, pos+off+16)
		if err != nil {
			return 0, 0, err
		}
		bs, be = le64(b[0:8]), le64(b[8:16])
	} else {
		off := uint64(a.blob) * 4
		b, err := a.z.bytesRangeAt(pos+off, pos+off+8)
		if err != nil {
			return 0, 0, err
		}
		bs, be = uint64(le32(b[0:4])), uint64(le32(b[4:8]))
	}

	// Checking be against size-pos (once pos is known to be in range) keeps
	// pos+be from overflowing as well as from running past the file.
	if pos > a.z.size || bs > be || be > a.z.size-pos {
		return 0, 0, fmt.Errorf("blob %d has invalid range [%d, %d) at offset %d", a.blob, bs, be, pos)
	}
	return pos + bs, pos + be, nil
}

// uncompressedRange returns where this entry's content sits in the file. It
// fails for entries in compressed clusters, whose bytes can't be read in place.
func (a *article) uncompressedRange() (uint64, uint64, error) {
	start, _, err := a.z.clusterOffsetsAtIdx(a.cluster)
	if err != nil {
		return 0, 0, err
	}
	info, err := a.z.bytesRangeAt(start, start+1)
	if err != nil {
		return 0, 0, err
	}
	if c := info[0] & 0x0f; c != 0 && c != 1 {
		return 0, 0, fmt.Errorf("cluster %d is compressed (type %d)", a.cluster, c)
	}
	return a.uncompressedBlobRange(start, info[0]&0x10 != 0)
}
