package remotecrawl

import (
	"context"
	"strconv"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawlurls"
)

func (b *Broker) selectLeaseRecords(
	observationContext context.Context,
	leaseContext context.Context,
	pending []queueRecord,
	limit int,
) ([]queueRecord, error) {
	selected := make([]queueRecord, 0, limit)
	for _, candidate := range pending {
		if len(selected) == limit || leaseSelectionStopped(leaseContext) {
			break
		}
		accepted, err := b.leaseRecordAllowed(leaseContext, candidate)
		if err != nil {
			return nil, err
		}
		if !accepted {
			b.observe(observationContext, Observation{
				Action: "lease", Outcome: "destination_rejected",
				URLHash: yagomodel.Hash(candidate.URLHash),
			}, true)
			continue
		}
		selected = append(selected, candidate)
	}

	return selected, nil
}

func (b *Broker) leaseRecordAllowed(
	ctx context.Context,
	candidate queueRecord,
) (bool, error) {
	canonical, err := b.policy.Admit(ctx, candidate.URL)
	if err == nil && canonical == candidate.URL {
		return true, nil
	}
	if err := b.deletePendingOrder(ctx, candidate); err != nil {
		return false, err
	}

	return false, nil
}

func remoteCrawlURLs(records []queueRecord) []crawlurls.RemoteCrawlURL {
	items := make([]crawlurls.RemoteCrawlURL, 0, len(records))
	for _, record := range records {
		items = append(items, crawlurls.RemoteCrawlURL{
			Link: record.URL, Referrer: record.Referrer,
			Description: record.Description,
			PublishedAt: publicationTime(record.PublishedAt),
			GUID:        yagomodel.Hash(record.URLHash),
		})
	}

	return items
}

func availableLeaseSlots(limit int, outstanding uint64) int {
	if limit < 1 || outstanding >= MaximumOutstandingPerPeer {
		return 0
	}
	used, _ := strconv.Atoi(strconv.FormatUint(outstanding, 10))
	if used >= limit {
		return 0
	}

	return limit - used
}

func leaseSelectionStopped(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}
