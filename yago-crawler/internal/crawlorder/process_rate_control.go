package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type ProcessRateControl struct {
	apply func(uint32)
	next  ControlHandler
}

func NewProcessRateControl(
	apply func(uint32),
	next ControlHandler,
) ProcessRateControl {
	return ProcessRateControl{apply: apply, next: next}
}

func (h ProcessRateControl) Apply(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) {
	h.ApplyControl(ctx, directive)
}

func (h ProcessRateControl) ApplyControl(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) bool {
	if directive.Kind == yagocrawlcontract.CrawlControlSetProcessRate {
		if h.apply != nil &&
			directive.ProcessPagesPerSecond <= yagocrawlcontract.MaximumProcessPagesPerSecond {
			h.apply(directive.ProcessPagesPerSecond)

			return true
		}

		return false
	}
	if h.next != nil {
		return applyControlDirective(ctx, h.next, directive)
	}

	return false
}
