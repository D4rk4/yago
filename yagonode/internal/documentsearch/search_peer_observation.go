package documentsearch

import (
	"context"
	"net/netip"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

type PotentialPeerObserver interface {
	ObservePotential(ctx context.Context, potential yagomodel.Seed)
}

func (e searchEndpoint) observePotentialPeer(
	ctx context.Context,
	req yagoproto.SearchRequest,
) {
	if e.potentialPeers == nil {
		return
	}
	potential, ok := potentialPeerFromRequest(ctx, req, e.identity.Hash)
	if !ok {
		return
	}
	e.potentialPeers.ObservePotential(ctx, potential)
}

func potentialPeerFromRequest(
	ctx context.Context,
	req yagoproto.SearchRequest,
	self yagomodel.Hash,
) (yagomodel.Seed, bool) {
	seed, found := req.MySeed.Get()
	if !found || seed.Hash == self {
		return yagomodel.Seed{}, false
	}
	iam, err := yagomodel.ParseHash(req.Iam)
	if err != nil || iam != seed.Hash {
		return yagomodel.Seed{}, false
	}
	port, found := seed.Port.Get()
	if !found {
		return yagomodel.Seed{}, false
	}
	address, err := netip.ParseAddr(httpguard.RemoteAddr(ctx))
	if err != nil {
		return yagomodel.Seed{}, false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return yagomodel.Seed{}, false
	}
	host, _ := yagomodel.ParseHost(address.String())

	return yagomodel.Seed{
		Hash:     seed.Hash,
		IP:       yagomodel.Some(host),
		Port:     yagomodel.Some(port),
		PeerType: yagomodel.Some(yagomodel.PeerVirgin),
	}, true
}
