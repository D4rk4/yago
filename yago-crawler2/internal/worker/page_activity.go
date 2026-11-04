package worker

import (
	"sort"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type pageActivity struct {
	url       string
	stage     string
	startedAt time.Time
}

func (worker *Worker) setPageActivity(pageID string, pageURL string, stage string) {
	worker.mutex.Lock()
	if worker.activePages == nil {
		worker.activePages = make(map[string]pageActivity)
	}
	activity, found := worker.activePages[pageID]
	if !found {
		activity.startedAt = time.Now()
	}
	activity.url = pageURL
	activity.stage = stage
	worker.activePages[pageID] = activity
	worker.mutex.Unlock()
}

func (worker *Worker) clearPageActivity(pageID string) {
	worker.mutex.Lock()
	delete(worker.activePages, pageID)
	worker.mutex.Unlock()
}

func (worker *Worker) activePageStatuses() []*crawlrpc.ActiveCrawlPage {
	worker.mutex.RLock()
	activities := make([]pageActivity, 0, len(worker.activePages))
	for _, activity := range worker.activePages {
		activities = append(activities, activity)
	}
	worker.mutex.RUnlock()
	sort.Slice(activities, func(left, right int) bool {
		if activities[left].startedAt.Equal(activities[right].startedAt) {
			return activities[left].url < activities[right].url
		}
		return activities[left].startedAt.Before(activities[right].startedAt)
	})
	if len(activities) > yagocrawlcontract.MaximumFetchWorkerConcurrency {
		activities = activities[:yagocrawlcontract.MaximumFetchWorkerConcurrency]
	}
	statuses := make([]*crawlrpc.ActiveCrawlPage, 0, len(activities))
	for _, activity := range activities {
		statuses = append(statuses, &crawlrpc.ActiveCrawlPage{
			Url:                 activity.url,
			Stage:               activity.stage,
			ElapsedMilliseconds: uint64(time.Since(activity.startedAt).Milliseconds()),
		})
	}
	return statuses
}
