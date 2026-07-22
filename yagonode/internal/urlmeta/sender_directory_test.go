package urlmeta

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
)

type acceptingSenderDirectory struct{}

func (acceptingSenderDirectory) PeerByHash(
	context.Context,
	yagomodel.Hash,
) (yagomodel.Seed, bool) {
	return yagomodel.Seed{}, true
}

type fixedSenderDirectory struct {
	peer yagomodel.Seed
}

func (d fixedSenderDirectory) PeerByHash(
	_ context.Context,
	peer yagomodel.Hash,
) (yagomodel.Seed, bool) {
	return d.peer, d.peer.Hash == peer
}
