package searchremote

import (
	"context"
	"net/url"

	"github.com/D4rk4/yago/yagoproto"
)

func sendEmptyRemoteSearchWithinLimit(
	searcher searcher,
	ctx context.Context,
	target *url.URL,
	callBudgets ...*outboundCallBudget,
) error {
	if !acquireOutboundCall(callBudgets...) {
		return errRemoteSearchBudgetExhausted
	}
	_, _, err := searcher.sendRemoteSearchToWithoutCallBudget(
		ctx,
		target,
		yagoproto.SearchRequest{},
		remoteSearchBodyCap,
	)

	return err
}
