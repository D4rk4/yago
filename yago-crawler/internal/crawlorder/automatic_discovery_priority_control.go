package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type AutomaticDiscoveryPriorityControlHandler struct {
	apply func(bool)
	next  ControlHandler
}

func NewAutomaticDiscoveryPriorityControlHandler(
	apply func(bool),
	next ControlHandler,
) AutomaticDiscoveryPriorityControlHandler {
	return AutomaticDiscoveryPriorityControlHandler{apply: apply, next: next}
}

func (h AutomaticDiscoveryPriorityControlHandler) Apply(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) {
	h.ApplyControl(ctx, directive)
}

func (h AutomaticDiscoveryPriorityControlHandler) ApplyControl(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) bool {
	if directive.Kind == yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority {
		if h.apply != nil {
			h.apply(directive.PrioritizeAutomaticDiscovery)

			return true
		}

		return false
	}
	if h.next != nil {
		return applyControlDirective(ctx, h.next, directive)
	}

	return false
}
