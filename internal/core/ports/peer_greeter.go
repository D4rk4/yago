package ports

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type GreetResult struct {
	YourIP   string
	YourType yacymodel.PeerType
	Known    []yacymodel.Seed
}

type PeerGreeter interface {
	Greet(ctx context.Context, endpoint string, self yacymodel.Seed, count int) (GreetResult, error)
}
