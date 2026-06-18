package contracts

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type HelloOutcome struct {
	CallerType yacymodel.PeerType
	Known      []yacymodel.Seed
}

type PeerDirectory interface {
	Hello(ctx context.Context, caller yacymodel.Seed, count int) (HelloOutcome, error)
}
