package frontier

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

type stagedPageHostProgress struct {
	host        string
	progress    frontiercheckpoint.HostProgress
	droppedURLs []string
}

func pageHostProgressKey(work crawljob.CrawlJob) string {
	if work.ObservationID != "" {
		return work.ObservationID
	}

	return work.URL
}

func (run *crawlRun) stagePageHostProgress(
	work crawljob.CrawlJob,
	host string,
	progress frontiercheckpoint.HostProgress,
	droppedURLs []string,
) {
	key := pageHostProgressKey(work)
	current, found := run.pageHostProgress[key]
	if found && current.progress.Pace.Generation > progress.Pace.Generation {
		progress.Pace = current.progress.Pace
		progress.PaceCapacity = current.progress.PaceCapacity
	}
	if found && len(droppedURLs) == 0 {
		droppedURLs = current.droppedURLs
	}
	run.pageHostProgress[key] = stagedPageHostProgress{
		host:        host,
		progress:    progress,
		droppedURLs: append([]string(nil), droppedURLs...),
	}
}

func (run *crawlRun) checkpointPageHostProgress(
	work crawljob.CrawlJob,
) *frontiercheckpoint.PageHostProgress {
	staged, found := run.pageHostProgress[pageHostProgressKey(work)]
	if !found {
		return nil
	}

	return &frontiercheckpoint.PageHostProgress{
		Host:        staged.host,
		Progress:    staged.progress,
		DroppedURLs: append([]string(nil), staged.droppedURLs...),
	}
}

func (run *crawlRun) clearPageHostProgress(work crawljob.CrawlJob) {
	delete(run.pageHostProgress, pageHostProgressKey(work))
}
