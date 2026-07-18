package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type ActiveRunControlHandler struct {
	apply func(int)
	next  ControlHandler
}

func NewActiveRunControlHandler(
	apply func(int),
	next ControlHandler,
) ActiveRunControlHandler {
	return ActiveRunControlHandler{apply: apply, next: next}
}

func (h ActiveRunControlHandler) Apply(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) {
	h.ApplyControl(ctx, directive)
}

func (h ActiveRunControlHandler) ApplyControl(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) bool {
	if directive.Kind == yagocrawlcontract.CrawlControlSetActiveRuns {
		maximum := int(directive.MaximumActiveRuns)
		if h.apply != nil && maximum >= 1 &&
			maximum <= yagocrawlcontract.MaximumActiveCrawlRunConcurrency {
			h.apply(maximum)

			return true
		}

		return false
	}
	if h.next != nil {
		return applyControlDirective(ctx, h.next, directive)
	}

	return false
}
