package yagonode

import (
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func dhtPublicReachability(
	assembly dhtOutboundRuntimeAssembly,
	self yagomodel.Hash,
	access yagoproto.NetworkAccess,
) publicReachability {
	direct := assembly.reachability
	if direct == nil {
		probe := newPublicEndpointSelfTest(
			assembly.client,
			assembly.config.NetworkName,
			self,
			assembly.config.PublicSelfTestURL,
			access,
		)
		probe.pinned = assembly.config.SelfTestURLPinned
		direct = probe
	}
	var external publicReachability
	if assembly.externalReachability != nil {
		external = peerBackPingPublicReachability{source: assembly.externalReachability}
	}

	return externalFirstPublicReachability{external: external, direct: direct}
}
