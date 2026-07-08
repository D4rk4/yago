package yacyproto

import (
	"crypto/md5"
	"encoding/hex"
)

const DefaultNetwork = "freeworld"

func NetworkUnit(name string) string {
	if name == "" {
		return DefaultNetwork
	}

	return name
}

func MagicMD5(key, iam, essentials string) string {
	sum := md5.Sum([]byte(key + iam + essentials))

	return hex.EncodeToString(sum[:])
}
