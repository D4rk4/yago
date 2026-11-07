package frontier

type pendingHostPages struct {
	host         string
	slot         int
	returned     []pendingPage
	returnedHead int
	queued       []pendingPage
	queuedHead   int
	retries      []pendingPage
	retriesHead  int
}

func (r *crawlRun) appendPending(candidate frontierCandidate) {
	bucket := r.pendingHost(candidate.host)
	bucket.queued = append(bucket.queued, pendingPageFromCandidate(candidate))
	r.pendingPages++
}

func (r *crawlRun) prependReturned(host string, pages []pendingPage) {
	bucket := r.pendingHost(host)
	existing := bucket.returned[bucket.returnedHead:]
	existingRetries := bucket.retries[bucket.retriesHead:]
	returned := make([]pendingPage, 0, len(pages)+len(existing))
	retries := make([]pendingPage, 0, len(pages)+len(existingRetries))
	for _, page := range pages {
		if page.retryAttempt == 0 {
			returned = append(returned, page)
		} else {
			retries = append(retries, page)
		}
	}
	returned = append(returned, existing...)
	retries = append(retries, existingRetries...)
	clear(bucket.returned)
	clear(bucket.retries)
	bucket.returned = returned
	bucket.returnedHead = 0
	bucket.retries = retries
	bucket.retriesHead = 0
	r.pendingPages += len(pages)
}

func (r *crawlRun) appendRetry(host string, page pendingPage) {
	bucket := r.pendingHost(host)
	bucket.retries = append(bucket.retries, page)
	r.pendingPages++
}

func (r *crawlRun) pendingHost(host string) *pendingHostPages {
	if bucket := r.pendingByHost[host]; bucket != nil {
		return bucket
	}
	bucket := &pendingHostPages{host: host, slot: len(r.pendingHosts)}
	r.pendingByHost[host] = bucket
	r.pendingHosts = append(r.pendingHosts, bucket)
	r.pendingHostLive++

	return bucket
}

func (r *crawlRun) popPending(
	accept func(string, pendingPage) bool,
) (string, pendingPage, bool) {
	start := r.pendingCursor % len(r.pendingHosts)
	for offset := range len(r.pendingHosts) {
		index := (start + offset) % len(r.pendingHosts)
		bucket := r.pendingHosts[index]
		if bucket == nil {
			continue
		}
		page := bucket.first()
		if accept != nil && !accept(bucket.host, page) {
			continue
		}
		r.pendingCursor = (index + 1) % len(r.pendingHosts)
		page = bucket.pop()
		r.pendingPages--
		if !bucket.hasPages() {
			delete(r.pendingByHost, bucket.host)
			r.pendingHosts[bucket.slot] = nil
			r.pendingHostLive--
			r.compactPendingHosts()
		}

		return bucket.host, page, true
	}

	return "", pendingPage{}, false
}

func (r *crawlRun) clearPending() int {
	pages := r.pendingPages
	for _, bucket := range r.pendingHosts {
		if bucket == nil {
			continue
		}
		r.removeFirstPassPending(bucket.firstPassPages())
		clear(bucket.returned)
		clear(bucket.queued)
		clear(bucket.retries)
	}
	clear(r.pendingHosts)
	clear(r.pendingByHost)
	r.pendingHosts = nil
	r.pendingByHost = make(map[string]*pendingHostPages)
	r.pendingCursor = 0
	r.pendingHostLive = 0
	r.pendingPages = 0

	return pages
}

func (r *crawlRun) clearPendingHost(host string) int {
	bucket := r.pendingByHost[host]
	if bucket == nil {
		return 0
	}
	pages := len(bucket.returned) - bucket.returnedHead +
		len(bucket.queued) - bucket.queuedHead + len(bucket.retries) - bucket.retriesHead
	r.removeFirstPassPending(bucket.firstPassPages())
	clear(bucket.returned)
	clear(bucket.queued)
	clear(bucket.retries)
	delete(r.pendingByHost, host)
	r.pendingHosts[bucket.slot] = nil
	r.pendingHostLive--
	r.pendingPages -= pages
	r.compactPendingHosts()

	return pages
}

func (r *crawlRun) removeFirstPassPending(count uint64) {
	if count > r.firstPassPending {
		r.firstPassPending = 0
		return
	}
	r.firstPassPending -= count
}

func (r *crawlRun) compactPendingHosts() {
	if r.pendingHostLive == 0 {
		clear(r.pendingHosts)
		r.pendingHosts = nil
		r.pendingCursor = 0

		return
	}
	tombstones := len(r.pendingHosts) - r.pendingHostLive
	if tombstones < frontierMutationBatchSize || r.pendingHostLive*2 > len(r.pendingHosts) {
		return
	}
	compacted := make([]*pendingHostPages, 0, r.pendingHostLive)
	start := r.pendingCursor % len(r.pendingHosts)
	for offset := range len(r.pendingHosts) {
		bucket := r.pendingHosts[(start+offset)%len(r.pendingHosts)]
		if bucket == nil {
			continue
		}
		bucket.slot = len(compacted)
		compacted = append(compacted, bucket)
	}
	clear(r.pendingHosts)
	r.pendingHosts = compacted
	r.pendingCursor = 0
}

func (p *pendingHostPages) first() pendingPage {
	if p.returnedHead < len(p.returned) {
		return p.returned[p.returnedHead]
	}

	if p.queuedHead < len(p.queued) {
		return p.queued[p.queuedHead]
	}
	return p.retries[p.retriesHead]
}

func (p *pendingHostPages) pop() pendingPage {
	if p.returnedHead < len(p.returned) {
		var page pendingPage
		page, p.returned, p.returnedHead = popPendingPage(p.returned, p.returnedHead)

		return page
	}
	if p.queuedHead < len(p.queued) {
		var page pendingPage
		page, p.queued, p.queuedHead = popPendingPage(p.queued, p.queuedHead)

		return page
	}
	var page pendingPage
	page, p.retries, p.retriesHead = popPendingPage(p.retries, p.retriesHead)

	return page
}

func (p *pendingHostPages) hasPages() bool {
	return p.returnedHead < len(p.returned) || p.queuedHead < len(p.queued) ||
		p.retriesHead < len(p.retries)
}

func (p *pendingHostPages) firstPassPages() uint64 {
	var count uint64
	for _, pages := range [][]pendingPage{
		p.returned[p.returnedHead:],
		p.queued[p.queuedHead:],
	} {
		for _, page := range pages {
			if page.retryAttempt == 0 {
				count++
			}
		}
	}
	return count
}

func pendingPageFromCandidate(candidate frontierCandidate) pendingPage {
	return pendingPage{
		normURL:          candidate.normURL,
		depth:            candidate.depth,
		profileHandle:    candidate.profileHandle,
		sourceModifiedAt: candidate.sourceModifiedAt,
		conditionalFetch: candidate.conditionalFetch,
		indexAllowed:     candidate.indexAllowed,
		observationID:    candidate.observationID,
		observedAt:       candidate.observedAt,
		retryAttempt:     candidate.retryAttempt,
		retryAfter:       candidate.retryAfter,
	}
}
