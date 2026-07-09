package manticoreindex

import (
	"crypto/sha256"
	"encoding/binary"
)

func documentIdentity(canonicalURL string) int64 {
	sum := sha256.Sum256([]byte(canonicalURL))
	return int64(binary.BigEndian.Uint64(sum[:8]) >> 1)
}
