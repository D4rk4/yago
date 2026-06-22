package main

import (
	"github.com/nikitakarpei/yacy-rwi-node/internal/infrastructure"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func nodeIdentity(config infrastructure.NodeConfig) yacymodel.PeerIdentity {
	return yacymodel.PeerIdentity{
		Hash:        config.Hash,
		NetworkName: config.NetworkName,
		Name:        config.Name,
		Host:        config.AdvertiseHost,
		Port:        config.AdvertisePort,
		Flags:       config.Flags,
	}
}
