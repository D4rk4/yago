package bootstrap

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type RuntimeStatus interface {
	SelfSeed(ctx context.Context) yacymodel.Seed
}

type TrustedSeedSink interface {
	Absorb(ctx context.Context, seeds ...yacymodel.Seed)
}

type GreetResult struct {
	YourIP   string
	YourType yacymodel.PeerType
	Known    []yacymodel.Seed
}
