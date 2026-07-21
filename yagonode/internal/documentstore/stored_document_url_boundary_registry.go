package documentstore

import (
	"slices"
	"sync"
)

const storedDocumentURLBoundaryCacheSize = 4096

type storedDocumentURLBoundaries struct {
	mutex     sync.Mutex
	entries   map[string]*storedDocumentURLBoundary
	idleFirst *storedDocumentURLBoundary
	idleLast  *storedDocumentURLBoundary
	idleTotal int
}

type storedDocumentURLBoundaryLease struct {
	url      string
	boundary *storedDocumentURLBoundary
}

func (b *storedDocumentURLBoundaries) retain(
	urls []string,
) []storedDocumentURLBoundaryLease {
	ordered := canonicalStoredDocumentBoundaryURLs(urls)
	leases := make([]storedDocumentURLBoundaryLease, 0, len(ordered))
	b.mutex.Lock()
	if b.entries == nil {
		b.entries = make(map[string]*storedDocumentURLBoundary, len(ordered))
	}
	for _, url := range ordered {
		boundary := b.entries[url]
		if boundary == nil {
			boundary = &storedDocumentURLBoundary{url: url}
			b.entries[url] = boundary
		} else if boundary.idle {
			b.removeIdle(boundary)
		}
		boundary.references++
		leases = append(leases, storedDocumentURLBoundaryLease{
			url:      url,
			boundary: boundary,
		})
	}
	b.mutex.Unlock()

	return leases
}

func canonicalStoredDocumentBoundaryURLs(urls []string) []string {
	if len(urls) == 1 {
		if urls[0] == "" {
			return nil
		}

		return urls
	}
	ordered := make([]string, 0, len(urls))
	for _, url := range urls {
		if url != "" {
			ordered = append(ordered, url)
		}
	}
	slices.Sort(ordered)

	return slices.Compact(ordered)
}

func (b *storedDocumentURLBoundaries) release(
	leases []storedDocumentURLBoundaryLease,
) {
	b.mutex.Lock()
	for _, lease := range leases {
		lease.boundary.references--
		if lease.boundary.references == 0 {
			b.appendIdle(lease.boundary)
		}
	}
	for b.idleTotal > storedDocumentURLBoundaryCacheSize {
		boundary := b.idleFirst
		b.removeIdle(boundary)
		if b.entries[boundary.url] == boundary {
			delete(b.entries, boundary.url)
		}
	}
	b.mutex.Unlock()
}

func (b *storedDocumentURLBoundaries) appendIdle(
	boundary *storedDocumentURLBoundary,
) {
	boundary.idle = true
	boundary.idlePrevious = b.idleLast
	boundary.idleNext = nil
	if b.idleLast == nil {
		b.idleFirst = boundary
	} else {
		b.idleLast.idleNext = boundary
	}
	b.idleLast = boundary
	b.idleTotal++
}

func (b *storedDocumentURLBoundaries) removeIdle(
	boundary *storedDocumentURLBoundary,
) {
	if boundary.idlePrevious == nil {
		b.idleFirst = boundary.idleNext
	} else {
		boundary.idlePrevious.idleNext = boundary.idleNext
	}
	if boundary.idleNext == nil {
		b.idleLast = boundary.idlePrevious
	} else {
		boundary.idleNext.idlePrevious = boundary.idlePrevious
	}
	boundary.idlePrevious = nil
	boundary.idleNext = nil
	boundary.idle = false
	b.idleTotal--
}
