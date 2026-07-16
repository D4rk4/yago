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
	if directive.Kind == yagocrawlcontract.CrawlControlSetWorkers {
		workers := int(directive.FetchWorkers)
		if h.apply != nil && workers >= 1 &&
			workers <= yagocrawlcontract.MaximumFetchWorkerConcurrency {
			h.apply(workers)
		}

		return
	}
	if h.next != nil {
		h.next.Apply(ctx, directive)
	}
}
