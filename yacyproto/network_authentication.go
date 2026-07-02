package yacyproto

import (
	"github.com/D4rk4/yago/yacymodel"
)

const DefaultNetwork = "freeworld"

func NetworkUnit(name string) string {
	if name == "" {
		return DefaultNetwork
	}

	return name
}

func MagicMD5(key, iam, essentials string) string {
	return yacymodel.YaCyHashHex(key + iam + essentials)
}
