package yacymodel

import (
	"crypto/md5"
	"errors"
	"fmt"
	"strings"
)

const HashLength = 12

const ReservedPrefix = "_____"

var ErrInvalidHash = errors.New("invalid hash")

type Hash string

func ParseHash(s string) (Hash, error) {
	if len(s) != HashLength {
		return "", fmt.Errorf("%w: length %d, want %d", ErrInvalidHash, len(s), HashLength)
	}
	for i := range len(s) {
		if decodeTable[s[i]] < 0 {
			return "", fmt.Errorf("%w: %q", ErrInvalidHash, s[i])
		}
	}
	return Hash(s), nil
}

func (h Hash) Valid() bool {
	_, err := ParseHash(string(h))
	return err == nil
}

func (h Hash) Reserved() bool {
	return strings.HasPrefix(string(h), ReservedPrefix)
}

func (h Hash) String() string { return string(h) }

func WordHash(word string) Hash {
	sum := md5.Sum([]byte(strings.ToLower(word)))
	return Hash(Encode(sum[:])[:HashLength])
}
