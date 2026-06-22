package bootstrap

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type StatusSnapshot struct {
	Seed yacymodel.Seed
}

type RuntimeStatus interface {
	Snapshot(ctx context.Context) StatusSnapshot
}

type TrustedSeedSink interface {
	Absorb(ctx context.Context, seeds ...yacymodel.Seed)
}

type GreetResult struct {
	YourIP   string
	YourType yacymodel.PeerType
	Known    []yacymodel.Seed
}
