package zim

import (
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

type ZstdReader struct {
	*zstd.Decoder
}

// NewZstdReader returns a decoder that refuses frames whose window or declared
// size exceeds the cluster size limit, so a crafted frame can't make the
// decoder itself allocate past it.
func NewZstdReader(r io.Reader) (*ZstdReader, error) {
	dec, err := zstd.NewReader(r, zstd.WithDecoderMaxMemory(uint64(maxDecompressedSize)))
	if err != nil {
		return nil, fmt.Errorf("can't read from zstd %w", err)
	}
	return &ZstdReader{dec}, nil
}

func (zr *ZstdReader) Close() error {
	zr.Decoder.Close()

	return nil
}
