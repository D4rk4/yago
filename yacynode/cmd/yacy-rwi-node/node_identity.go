package main

import (
	"time"

	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
)

func nodeIdentity(config nodeConfig) nodeidentity.Identity {
	return nodeidentity.Identity{
		Hash:        config.Hash,
		NetworkName: config.NetworkName,
		Name:        config.Name,
		Host:        config.AdvertiseHost,
		Port:        config.AdvertisePort,
		Flags:       config.Flags,
		Version:     version,
		Start:       time.Now(),
	}
}
