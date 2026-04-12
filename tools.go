package zim

import (
	"encoding/binary"
)

// le16 reads a little-endian uint16 from b. Caller must ensure len(b) >= 2.
func le16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }

// le32 reads a little-endian uint32 from b. Caller must ensure len(b) >= 4.
func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

// le64 reads a little-endian uint64 from b. Caller must ensure len(b) >= 8.
func le64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }
