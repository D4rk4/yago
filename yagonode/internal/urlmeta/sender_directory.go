package urlmeta

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
)

type SenderDirectory interface {
	PeerByHash(context.Context, yagomodel.Hash) (yagomodel.Seed, bool)
}
