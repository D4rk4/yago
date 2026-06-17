package yacyproto

import (
	"crypto/md5" //nolint:gosec // YaCy network magic is defined as MD5; not a security control.
	"encoding/hex"
)

// DefaultNetwork is the network unit assumed when a request omits FieldNetworkName.
const DefaultNetwork = "freeworld"

// NetworkUnit resolves a FieldNetworkName value to its network unit, defaulting
// to DefaultNetwork when the value is empty.
func NetworkUnit(name string) string {
	if name == "" {
		return DefaultNetwork
	}

	return name
}

// MagicMD5 derives the controlled-network authentication value sent in
// FieldMagicMD5: the hex MD5 of the shared key, the sender peer hash, and the
// network essentials concatenated in that order.
func MagicMD5(key, iam, essentials string) string {
	sum := md5.Sum([]byte(key + iam + essentials)) //nolint:gosec // see import.

	return hex.EncodeToString(sum[:])
}
