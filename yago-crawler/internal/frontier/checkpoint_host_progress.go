package frontier

import (
	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
)

func (f *Frontier) queuedHostURLsLocked(runID uuid.UUID, host string) []string {
	urls := make([]string, 0)
	for _, job := range f.state.ready {
		if job.RunID == runID && weburl.Host(job.URL) == host {
			urls = append(urls, job.URL)
		}
	}
	run := f.state.runs[runID]
	if run == nil {
		return urls
	}
	bucket := run.pendingByHost[host]
	if bucket == nil {
		return urls
	}
	for _, page := range bucket.returned[bucket.returnedHead:] {
		urls = append(urls, page.normURL)
	}
	for _, page := range bucket.queued[bucket.queuedHead:] {
		urls = append(urls, page.normURL)
	}

	return urls
}
