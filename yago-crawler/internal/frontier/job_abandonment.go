package frontier

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
)

func (f *Frontier) Abandon(work crawljob.CrawlJob) {
	f.mu.Lock()
	f.abandonJobLocked(work, f.state.runs[work.RunID])
	f.mu.Unlock()
	f.wake()
}
