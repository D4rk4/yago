package yacymodel

import (
	"crypto/md5"
	"encoding/hex"
)

func YaCyHashBase64(raw string) string {
	sum := md5.Sum([]byte(raw))
	return Encode(sum[:])
}

func YaCyHashHex(raw string) string {
	sum := md5.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}
