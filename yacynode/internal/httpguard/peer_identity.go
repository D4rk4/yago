package httpguard

import (
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type PeerIdentity struct {
	Hash        yacymodel.Hash
	NetworkName string
}

func (p PeerIdentity) NetworkMatches(network string) bool {
	return yacyproto.NetworkUnit(network) == yacyproto.NetworkUnit(p.NetworkName)
}

func (p PeerIdentity) YouAreMatches(youare yacymodel.Hash) bool {
	return youare == p.Hash
}
