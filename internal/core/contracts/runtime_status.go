package contracts

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type StatusSnapshot struct {
	Version string
	Uptime  int
	Seed    yacymodel.Seed
}

type RuntimeStatus interface {
	Snapshot(ctx context.Context) StatusSnapshot
}
