package crawlbroker

import (
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func crawlURLOutcomeHistoryFromProto(
	encoded []*crawlrpc.CrawlURLOutcome,
	workerSessionID string,
) (yagocrawlcontract.CrawlURLOutcomeHistory, error) {
	if len(encoded) > yagocrawlcontract.MaximumRecentCrawlURLOutcomes {
		return yagocrawlcontract.CrawlURLOutcomeHistory{}, fmt.Errorf(
			"crawl URL outcome history exceeds %d entries",
			yagocrawlcontract.MaximumRecentCrawlURLOutcomes,
		)
	}
	outcomes := make([]yagocrawlcontract.CrawlURLOutcome, 0, len(encoded))
	for _, value := range encoded {
		if value == nil || value.GetObservedAtUnixMilliseconds() <= 0 {
			return yagocrawlcontract.CrawlURLOutcomeHistory{}, fmt.Errorf(
				"invalid crawl URL outcome",
			)
		}
		outcomes = append(outcomes, yagocrawlcontract.CrawlURLOutcome{
			Sequence:        value.GetSequence(),
			URL:             value.GetUrl(),
			Class:           yagocrawlcontract.CrawlURLOutcomeClass(value.GetOutcomeClass()),
			ObservedAt:      time.UnixMilli(value.GetObservedAtUnixMilliseconds()).UTC(),
			HTTPStatus:      value.GetHttpStatus(),
			Reason:          value.GetReason(),
			WorkerSessionID: workerSessionID,
		})
	}
	history, err := yagocrawlcontract.NewCrawlURLOutcomeHistory(outcomes)
	if err != nil {
		return yagocrawlcontract.CrawlURLOutcomeHistory{}, fmt.Errorf(
			"validate crawl URL outcome history: %w",
			err,
		)
	}

	return history, nil
}
