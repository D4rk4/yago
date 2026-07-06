package shardvault

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"

	"github.com/klauspost/compress/zstd"
)

// Stored values carry a one-byte format tag. Raw values append a crc32c so
// every stored byte is integrity-checked; compressed values rely on the zstd
// frame's built-in content checksum.
const (
	tagRaw  = 0x00
	tagZstd = 0x01

	// compressMinBytes skips compression for values too small to gain.
	compressMinBytes = 64
	// compressMinSavingDivisor requires at least a 1/8 saving to keep the
	// compressed form.
	compressMinSavingDivisor = 8
)

var (
	errValueChecksum = errors.New("stored value failed its checksum")
	errValueFormat   = errors.New("stored value has an unknown format tag")

	crcTable = crc32.MakeTable(crc32.Castagnoli)

	// The encoder and decoder are concurrency-safe with EncodeAll/DecodeAll.
	zstdEncoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	zstdDecoder, _ = zstd.NewReader(nil)
)

// encodeValue compresses when it pays, otherwise stores raw with a checksum.
func encodeValue(value []byte) []byte {
	if len(value) >= compressMinBytes {
		compressed := zstdEncoder.EncodeAll(value, make([]byte, 1, len(value)/2+1))
		compressed[0] = tagZstd
		if len(compressed) <= len(value)-len(value)/compressMinSavingDivisor {
			return compressed
		}
	}
	out := make([]byte, 1+4+len(value))
	out[0] = tagRaw
	binary.BigEndian.PutUint32(out[1:5], crc32.Checksum(value, crcTable))
	copy(out[5:], value)

	return out
}

// decodeValue restores a stored value, verifying its integrity. A nil stored
// slice (absent key) stays nil.
func decodeValue(stored []byte) ([]byte, error) {
	if stored == nil {
		return nil, nil
	}
	if len(stored) == 0 {
		return nil, errValueFormat
	}
	switch stored[0] {
	case tagRaw:
		if len(stored) < 5 {
			return nil, errValueFormat
		}
		value := stored[5:]
		if crc32.Checksum(value, crcTable) != binary.BigEndian.Uint32(stored[1:5]) {
			return nil, errValueChecksum
		}

		return value, nil
	case tagZstd:
		value, err := zstdDecoder.DecodeAll(stored[1:], nil)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errValueChecksum, err)
		}

		return value, nil
	default:
		return nil, errValueFormat
	}
}
