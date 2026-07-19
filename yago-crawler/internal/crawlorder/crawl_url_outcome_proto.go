package crawlorder

import (
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func protoCrawlURLOutcomes(
	history yagocrawlcontract.CrawlURLOutcomeHistory,
) []*crawlrpc.CrawlURLOutcome {
	outcomes := history.Chronological()
	encoded := make([]*crawlrpc.CrawlURLOutcome, 0, len(outcomes))
	for _, outcome := range outcomes {
		encoded = append(encoded, &crawlrpc.CrawlURLOutcome{
			Sequence:                   outcome.Sequence,
			Url:                        outcome.URL,
			OutcomeClass:               string(outcome.Class),
			ObservedAtUnixMilliseconds: outcome.ObservedAt.UnixMilli(),
			HttpStatus:                 outcome.HTTPStatus,
			Reason:                     outcome.Reason,
		})
	}

	return encoded
}
