package yagomodel

import (
	"crypto/md5"
	"encoding/hex"
)

// YaCyHashBase64 derives the YaCy wire identifier for a value. MD5 here is the
// peer protocol's fixed, non-cryptographic name-derivation function: every YaCy
// peer computes the same digest for interoperable word and URL hashes, so no
// collision resistance is relied upon and the algorithm cannot change without
// breaking wire compatibility.
func YaCyHashBase64(raw string) string {
	// nosemgrep
	sum := md5.Sum([]byte(raw))

	return Encode(sum[:])
}

// YaCyHashHex is YaCyHashBase64 with the digest in hex, as some YaCy surfaces
// expect; the same wire-compatibility constraint applies.
func YaCyHashHex(raw string) string {
	// nosemgrep
	sum := md5.Sum([]byte(raw))

	return hex.EncodeToString(sum[:])
}
