package yacymodel

import (
	"crypto/md5" //nolint:gosec // YaCy word hashes are defined as MD5; not a security control.
	"errors"
	"fmt"
	"strings"
)

// HashLength is the fixed width, in enhanced-Base64 symbols, of every YaCy
// word, URL, and peer hash.
const HashLength = 12

// ReservedPrefix marks reserved/private hashes.
const ReservedPrefix = "_____"

// ErrInvalidHash reports a value that is not a well-formed YaCy hash.
var ErrInvalidHash = errors.New("invalid hash")

// Hash is a fixed-width 12-symbol enhanced-Base64 YaCy hash.
type Hash string

// ParseHash validates s and returns it as a Hash.
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

// Valid reports whether h is a well-formed YaCy hash.
func (h Hash) Valid() bool {
	_, err := ParseHash(string(h))
	return err == nil
}

// Reserved reports whether h is in the reserved/private range.
func (h Hash) Reserved() bool {
	return strings.HasPrefix(string(h), ReservedPrefix)
}

// String returns h as a string.
func (h Hash) String() string { return string(h) }

// WordHash derives the YaCy word hash of word: the first HashLength symbols of
// the enhanced-Base64 encoding of the MD5 of the lower-cased word.
func WordHash(word string) Hash {
	sum := md5.Sum([]byte(strings.ToLower(word))) //nolint:gosec // see import.
	return Hash(Encode(sum[:])[:HashLength])
}
