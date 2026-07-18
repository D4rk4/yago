package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type WorkerConcurrencyControlHandler struct {
	apply func(int)
	next  ControlHandler
}

func NewWorkerConcurrencyControlHandler(
	apply func(int),
	next ControlHandler,
) WorkerConcurrencyControlHandler {
	return WorkerConcurrencyControlHandler{apply: apply, next: next}
}

func (h WorkerConcurrencyControlHandler) Apply(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) {
	h.ApplyControl(ctx, directive)
}

func (h WorkerConcurrencyControlHandler) ApplyControl(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) bool {
	if directive.Kind == yagocrawlcontract.CrawlControlSetWorkers {
		workers := int(directive.FetchWorkers)
		if h.apply != nil && workers >= 1 &&
			workers <= yagocrawlcontract.MaximumFetchWorkerConcurrency {
			h.apply(workers)

			return true
		}

		return false
	}
	if h.next != nil {
		return applyControlDirective(ctx, h.next, directive)
	}

	return false
}
