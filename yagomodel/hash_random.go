package yagomodel

import (
	"crypto/rand"
	"fmt"
	"io"
)

const hashEntropyBytes = HashLength * 6 / 8

func GenerateHash(entropy io.Reader) (Hash, error) {
	buf := make([]byte, hashEntropyBytes)
	if _, err := io.ReadFull(entropy, buf); err != nil {
		return "", fmt.Errorf("read entropy: %w", err)
	}
	return ParseHash(Encode(buf))
}

func NewHash() (Hash, error) {
	return GenerateHash(rand.Reader)
}
