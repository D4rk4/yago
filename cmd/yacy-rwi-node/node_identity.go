package main

import "github.com/nikitakarpei/yacy-rwi-node/internal/nodestatus"

func nodeIdentity(config nodeConfig) nodestatus.Identity {
	return nodestatus.Identity{
		Hash:        config.Hash,
		NetworkName: config.NetworkName,
		Name:        config.Name,
		Host:        config.AdvertiseHost,
		Port:        config.AdvertisePort,
		Flags:       config.Flags,
		Version:     version,
	}
}
