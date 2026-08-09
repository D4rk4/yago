package pipeline

import "github.com/D4rk4/yago/yago-crawler/internal/crawljob"

type lostLeaseRunSuspender interface {
	SuspendLostLeaseRun(crawljob.CrawlJob) bool
}

func suspendUngrantedRun(frontier Frontier, job crawljob.CrawlJob) bool {
	suspender, available := frontier.(lostLeaseRunSuspender)
	if !available {
		frontier.Abandon(job)

		return false
	}

	return suspender.SuspendLostLeaseRun(job)
}
