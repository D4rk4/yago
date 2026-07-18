package frontier

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
)

type redirectReservation struct {
	URL      string
	Host     string
	HostBump bool
}

type redirectResolution struct {
	normalized string
	direct     bool
	previous   redirectReservation
	redirected bool
	checkpoint frontiercheckpoint.Redirect
}

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
	sourceURL, sourceOK := weburl.Normalize(job.URL)
	direct := sourceOK && norm == sourceURL

	run, durable := f.acquireRunDurability(job.RunID)
	f.mu.Lock()
	if f.state.runs[job.RunID] != run || run == nil {
		f.mu.Unlock()
		f.wake()

		return true
	}
	if !runLeaseMatchesJob(run, job) {
		if durable {
			f.finishRunDurabilityLocked(job.RunID, run, nil)
		}
		f.mu.Unlock()
		f.wake()

		return false
	}
	previous, redirected := run.redirects[job.URL]
	if direct && !redirected {
		return f.finishRedirectLocked(job, run, durable, nil, true)
	}
	redirect := frontiercheckpoint.Redirect{SourceURL: job.URL}
	if !direct {
		redirect.FinalURL = norm
		redirect.FinalHost = weburl.Host(norm)
		redirect.IncrementHost = redirect.FinalHost != weburl.Host(job.URL)
	}
	resolution := redirectResolution{
		normalized: norm,
		direct:     direct,
		previous:   previous,
		redirected: redirected,
		checkpoint: redirect,
	}
	if durable {
		return f.resolveDurableRedirectLocked(job, run, resolution)
	}

	return f.resolveMemoryRedirectLocked(job, run, resolution)
}

func (f *Frontier) resolveDurableRedirectLocked(
	job crawljob.CrawlJob,
	run *crawlRun,
	resolution redirectResolution,
) bool {
	f.mu.Unlock()
	recorded, err := f.checkpoint.RecordRedirect(
		context.Background(),
		job.Provenance,
		resolution.checkpoint,
	)
	f.mu.Lock()
	if err != nil {
		return f.finishRedirectLocked(job, run, true, err, false)
	}
	if f.state.runs[job.RunID] != run {
		return f.finishRedirectLocked(job, run, true, nil, true)
	}
	if resolution.redirected &&
		resolution.previous.URL != resolution.checkpoint.FinalURL {
		if err := releaseRedirectInMemory(run, job.URL, resolution.previous); err != nil {
			return f.finishRedirectLocked(job, run, true, err, false)
		}
	}
	if resolution.direct {
		return f.finishRedirectLocked(job, run, true, nil, true)
	}
	if !recorded {
		return f.finishRedirectLocked(job, run, true, nil, false)
	}
	if resolution.redirected && resolution.previous.URL == resolution.normalized {
		return f.finishRedirectLocked(job, run, true, nil, true)
	}
	recordRedirectInMemory(run, job.URL, resolution.checkpoint)

	return f.finishRedirectLocked(job, run, true, nil, true)
}

func (f *Frontier) resolveMemoryRedirectLocked(
	job crawljob.CrawlJob,
	run *crawlRun,
	resolution redirectResolution,
) bool {
	if resolution.redirected {
		if resolution.previous.URL == resolution.normalized {
			f.mu.Unlock()

			return true
		}
		_ = releaseRedirectInMemory(run, job.URL, resolution.previous)
	}
	if resolution.direct {
		f.mu.Unlock()

		return true
	}
	if _, seen := run.visited[resolution.normalized]; seen {
		f.mu.Unlock()

		return false
	}
	recordRedirectInMemory(run, job.URL, resolution.checkpoint)
	f.mu.Unlock()

	return true
}

func (f *Frontier) finishRedirectLocked(
	job crawljob.CrawlJob,
	run *crawlRun,
	durable bool,
	err error,
	resolved bool,
) bool {
	if durable {
		f.finishRunDurabilityLocked(job.RunID, run, err)
	}
	f.mu.Unlock()
	f.wake()

	return resolved
}

func recordRedirectInMemory(
	run *crawlRun,
	sourceURL string,
	redirect frontiercheckpoint.Redirect,
) {
	if run.redirects == nil {
		run.redirects = make(map[string]redirectReservation)
	}
	run.redirects[sourceURL] = redirectReservation{
		URL:      redirect.FinalURL,
		Host:     redirect.FinalHost,
		HostBump: redirect.IncrementHost,
	}
	if !run.boundedRecovery {
		run.visited[redirect.FinalURL] = struct{}{}
	}
	if redirect.IncrementHost {
		run.hostPages[redirect.FinalHost]++
	}
	run.retainBoundedResidentRedirect(run.redirects[sourceURL])
}

func releaseRedirectInMemory(
	run *crawlRun,
	sourceURL string,
	redirect redirectReservation,
) error {
	delete(run.redirects, sourceURL)
	delete(run.visited, redirect.URL)
	if !redirect.HostBump {
		run.releaseBoundedResidentHost(redirect.Host)

		return nil
	}
	if run.hostPages[redirect.Host] == 0 {
		return fmt.Errorf(
			"%w: redirect host total is empty",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	run.hostPages[redirect.Host]--
	run.releaseBoundedResidentHost(redirect.Host)

	return nil
}
