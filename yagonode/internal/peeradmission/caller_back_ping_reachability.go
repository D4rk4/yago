package peeradmission

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
)

func (p callerBackPing) Reachable(
	ctx context.Context,
	caller yagomodel.Seed,
	self yagomodel.Hash,
	networkName string,
) bool {
	_, answered := p.ReachableCaller(
		ctx, caller, self, networkName, yagomodel.None[peerwire.Endpoint](),
	)

	return answered.Present()
}
