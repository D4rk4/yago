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
	if directive.Kind == yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority {
		if h.apply != nil {
			h.apply(directive.PrioritizeAutomaticDiscovery)
		}

		return
	}
	if h.next != nil {
		h.next.Apply(ctx, directive)
	}
}
