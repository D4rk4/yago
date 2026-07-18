package crawlbroker

import "context"

type authorizedLeaseMutationGroupKey struct{}

func (q *DurableOrderQueue) beginAuthorizedLeaseMutationGroup(
	ctx context.Context,
) (context.Context, func()) {
	q.leaseMutation.RLock()

	return context.WithValue(ctx, authorizedLeaseMutationGroupKey{}, q), q.leaseMutation.RUnlock
}

func (q *DurableOrderQueue) authorizedLeaseMutationGroup(ctx context.Context) bool {
	group, _ := ctx.Value(authorizedLeaseMutationGroupKey{}).(*DurableOrderQueue)

	return group == q
}
