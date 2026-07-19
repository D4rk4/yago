package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type MaximumRedirectsControl struct {
	apply func(int)
	next  ControlHandler
}

func NewMaximumRedirectsControl(
	apply func(int),
	next ControlHandler,
) MaximumRedirectsControl {
	return MaximumRedirectsControl{apply: apply, next: next}
}

func (h MaximumRedirectsControl) Apply(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) {
	h.ApplyControl(ctx, directive)
}

func (h MaximumRedirectsControl) ApplyControl(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) bool {
	if directive.Kind == yagocrawlcontract.CrawlControlSetMaximumRedirects {
		if h.apply != nil && directive.MaximumRedirects <= yagocrawlcontract.MaximumPageRedirects {
			h.apply(int(directive.MaximumRedirects))

			return true
		}

		return false
	}
	if h.next != nil {
		return applyControlDirective(ctx, h.next, directive)
	}

	return false
}
