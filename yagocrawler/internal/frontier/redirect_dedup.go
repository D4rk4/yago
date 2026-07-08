package frontier

import (
	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

// ResolveRedirect records a job's post-redirect final URL against its run's
// visited-set, so two URLs redirecting to one target index it once per run. A
// false return means the final URL was already visited this run (counted as a
// duplicate) and the caller must skip indexing and link discovery. A fresh
// final URL is recorded, bumping the run's per-host page count only when the
// final host differs from the job's original host, and admits the page. An
// unparseable final URL or an unknown (already completed) run admits the page
// unchecked so in-flight work never breaks.
func (f *Frontier) ResolveRedirect(job crawljob.CrawlJob, finalURL string) bool {
	norm, ok := weburl.Normalize(finalURL)
	if !ok {
		return true
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.state.runs[job.RunID]
	if !ok {
		return true
	}
	if _, seen := run.visited[norm]; seen {
		f.state.tally.Duplicate(job.Provenance)

		return false
	}
	run.visited[norm] = struct{}{}
	if host := weburl.Host(norm); host != weburl.Host(job.URL) {
		run.hostPages[host]++
	}

	return true
}
