package yagoproto

import (
	"github.com/D4rk4/yago/yagomodel"
)

const DefaultNetwork = "freeworld"

func NetworkUnit(name string) string {
	if name == "" {
		return DefaultNetwork
	}

	return name
}

func MagicMD5(key, iam, essentials string) string {
	return yagomodel.YaCyHashHex(key + iam + essentials)
}
