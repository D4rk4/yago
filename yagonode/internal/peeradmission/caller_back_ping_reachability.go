package peeradmission

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
)

func (p callerBackPing) Reachable(
	ctx context.Context,
	caller yagomodel.Seed,
	self yagomodel.Hash,
	networkName string,
) bool {
	_, reachable := p.ReachableCaller(ctx, caller, self, networkName)

	return reachable
}
