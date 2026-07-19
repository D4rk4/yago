package yagonode

import (
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func configuredNetworkAccess(config nodeConfig, self yagomodel.Hash) yagoproto.NetworkAccess {
	return yagoproto.NetworkAccess{
		NetworkName: config.NetworkName,
		Mode:        config.NetworkAuthenticationMode,
		Essentials:  config.NetworkAuthenticationSecret,
		Self:        self,
	}
}
