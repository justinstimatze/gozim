package zim

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// newTestReader returns a zimReader over raw bytes, skipping header parsing,
// so individual read paths can be fed malformed clusters directly.
func newTestReader(t *testing.T, data []byte, mmap bool) *zimReader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "raw")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	z := &zimReader{f: f, size: uint64(len(data))}
	if mmap {
		z.mmap = data
	}
	return z
}

// uncompressedCluster builds a cluster: info byte, then blob offset pairs.
func uncompressedCluster(extended bool, bs, be uint64) []byte {
	if extended {
		b := make([]byte, 1+16+16)
		b[0] = 0x11
		binary.LittleEndian.PutUint64(b[1:], bs)
		binary.LittleEndian.PutUint64(b[9:], be)
		return b
	}
	b := make([]byte, 1+8+16)
	b[0] = 0x01
	binary.LittleEndian.PutUint32(b[1:], uint32(bs))
	binary.LittleEndian.PutUint32(b[5:], uint32(be))
	return b
}

func TestReadUncompressedMalformedOffsets(t *testing.T) {
	cases := []struct {
		name     string
		extended bool
		bs, be   uint64
	}{
		{"reversed", false, 10, 2},
		{"reversed extended", true, 20, 2},
		{"end past file", false, 8, 1 << 31},
		{"end past file extended", true, 16, 1 << 40},
		{"start overflows", true, ^uint64(0), 4},
	}
	for _, tc := range cases {
		for _, mmap := range []bool{false, true} {
			name := tc.name
			if mmap {
				name += " mmap"
			}
			t.Run(name, func(t *testing.T) {
				z := newTestReader(t, uncompressedCluster(tc.extended, tc.bs, tc.be), mmap)
				a := &article{z: z}
				if _, err := a.readUncompressed(0, tc.extended); err == nil {
					t.Fatalf("offsets (%d, %d): expected error", tc.bs, tc.be)
				}
			})
		}
	}
}

func TestReadUncompressedValid(t *testing.T) {
	data := uncompressedCluster(false, 8, 12)
	// Blob 0 spans [8,12) relative to the offset table, which starts at 1.
	copy(data[9:13], "WXYZ")
	z := newTestReader(t, data, false)
	got, err := (&article{z: z}).readUncompressed(0, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "WXYZ" {
		t.Errorf("got %q, want WXYZ", got)
	}
}

func TestSafeBlobOffsetsRange(t *testing.T) {
	blob := func(bs, be uint32) []byte {
		b := make([]byte, 16)
		binary.LittleEndian.PutUint32(b[0:], bs)
		binary.LittleEndian.PutUint32(b[4:], be)
		return b
	}
	if _, _, err := safeBlobOffsets(blob(10, 2), 0, false); err == nil {
		t.Error("reversed offsets: expected error")
	}
	if _, _, err := safeBlobOffsets(blob(8, 100), 0, false); err == nil {
		t.Error("end past blob: expected error")
	}
	if s, e, err := safeBlobOffsets(blob(8, 12), 0, false); err != nil || s != 8 || e != 12 {
		t.Errorf("valid offsets: got (%d, %d, %v)", s, e, err)
	}
}

func TestBytesRangeAtBounds(t *testing.T) {
	for _, mmap := range []bool{false, true} {
		z := newTestReader(t, make([]byte, 32), mmap)
		if _, err := z.bytesRangeAt(10, 2); err == nil {
			t.Errorf("mmap=%v: end < start: expected error", mmap)
		}
		if _, err := z.bytesRangeAt(0, 1<<40); err == nil {
			t.Errorf("mmap=%v: end past file: expected error", mmap)
		}
		if b, err := z.bytesRangeAt(4, 8); err != nil || len(b) != 4 {
			t.Errorf("mmap=%v: valid range: got len %d, err %v", mmap, len(b), err)
		}
	}
}
