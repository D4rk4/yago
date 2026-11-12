package worker

import (
	"context"
	"errors"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type seedAdmission struct {
	receivedRequests    int
	admittedPages       int
	filteredRequests    int
	hostLimitedRequests int
	duplicateRequests   int
}

func admitOrderSeeds(
	ctx context.Context,
	workFrontier *frontier.Frontier,
	runID string,
	requests []yagocrawlcontract.CrawlRequest,
	rules runRules,
) (seedAdmission, error) {
	admission := seedAdmission{receivedRequests: len(requests)}
	for _, request := range requests {
		mode, valid := yagocrawlcontract.NormalizeCrawlRequestMode(request.Mode)
		if !valid || mode == yagocrawlcontract.CrawlRequestModeRobots || request.Depth < 0 ||
			!rules.allowsCrawlURL(request.URL) {
			admission.filteredRequests++
			continue
		}
		_, added, err := workFrontier.EnqueueRunLimited(
			ctx,
			runID,
			request.URL,
			request.ReferrerURL,
			request.Depth,
			time.Now(),
			rules.maximumHostPages,
			rules.maximumPages,
		)
		if errors.Is(err, frontier.ErrHostPageLimit) || errors.Is(err, frontier.ErrRunPageLimit) {
			admission.hostLimitedRequests++
			continue
		}
		if err != nil {
			return admission, err
		}
		if added {
			admission.admittedPages++
		} else {
			admission.duplicateRequests++
		}
	}
	return admission, nil
}
