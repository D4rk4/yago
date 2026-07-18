package frontier

import "github.com/D4rk4/yago/yago-crawler/internal/weburl"

func (run *crawlRun) retainBoundedResidentHost(host string) {
	run.residentHostReferences[host]++
}

func (run *crawlRun) releaseBoundedResidentHost(host string) {
	if !run.boundedRecovery || host == "" {
		return
	}
	if run.residentHostReferences[host] > 1 {
		run.residentHostReferences[host]--

		return
	}
	delete(run.residentHostReferences, host)
	run.evictBoundedHostState(host)
}

func (run *crawlRun) retainBoundedResidentPage(pageURL string) {
	if !run.boundedRecovery {
		return
	}
	run.retainBoundedResidentHost(weburl.Host(pageURL))
}

func (run *crawlRun) releaseBoundedResidentPage(pageURL string) {
	if !run.boundedRecovery {
		return
	}
	delete(run.visited, pageURL)
	if redirect, found := run.redirects[pageURL]; found {
		delete(run.redirects, pageURL)
		delete(run.visited, redirect.URL)
		run.releaseBoundedResidentHost(redirect.Host)
	}
	run.releaseBoundedResidentHost(weburl.Host(pageURL))
}

func (run *crawlRun) retainBoundedResidentRedirect(redirect redirectReservation) {
	if !run.boundedRecovery {
		return
	}
	run.retainBoundedResidentHost(redirect.Host)
}

func (run *crawlRun) evictBoundedColdHostStates() {
	for host := range run.hostPages {
		run.evictBoundedHostState(host)
	}
}

func (run *crawlRun) evictBoundedHostState(host string) {
	if run.residentHostReferences[host] > 0 {
		return
	}
	delete(run.hostPages, host)
	delete(run.hostFailures, host)
	delete(run.hostGenerations, host)
	delete(run.retiredHosts, host)
}

func (run *crawlRun) releaseBoundedPendingPages() {
	for _, bucket := range run.pendingByHost {
		for _, page := range bucket.returned[bucket.returnedHead:] {
			run.releaseBoundedResidentPage(page.normURL)
		}
		for _, page := range bucket.queued[bucket.queuedHead:] {
			run.releaseBoundedResidentPage(page.normURL)
		}
	}
}

func (run *crawlRun) releaseBoundedPendingHost(host string) {
	bucket := run.pendingByHost[host]
	if bucket == nil {
		return
	}
	for _, page := range bucket.returned[bucket.returnedHead:] {
		run.releaseBoundedResidentPage(page.normURL)
	}
	for _, page := range bucket.queued[bucket.queuedHead:] {
		run.releaseBoundedResidentPage(page.normURL)
	}
}
