package yagonode

import (
	"context"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

// remoteResultStore persists metadata from peer (remote) search results into the
// local index. It is an interface so the caching decorator can be exercised with
// a recording fake, independent of the document store and full-text backend.
type remoteResultStore interface {
	store(ctx context.Context, results []searchcore.Result)
}

// remoteIndexCache mirrors YaCy's addResultsToLocalIndex: when a search fans out
// to the DHT swarm, the metadata of the results a peer returned is written into
// the local document store and search index so a later query finds them locally
// without re-fetching. It stores metadata only (title, snippet, url — no crawl)
// and never overwrites a document that is already stored, so a locally crawled
// full page is never clobbered by a lighter remote stub (YaCy commit 796770e).
type remoteIndexCache struct {
	directory documentstore.DocumentDirectory
	receiver  documentstore.DocumentReceiver
	index     searchindex.SearchIndex
	now       func() time.Time
	admission growthAdmission
}

func newRemoteIndexCache(
	storage nodeStorage,
	admissions ...growthAdmission,
) remoteIndexCache {
	cache := remoteIndexCache{
		directory: storage.documentDirectory,
		receiver:  storage.documentReceiver,
		index:     storage.searchIndex,
		now:       time.Now,
	}
	if len(admissions) > 0 {
		cache.admission = admissions[0]
	}

	return cache
}

func (c remoteIndexCache) store(ctx context.Context, results []searchcore.Result) {
	for _, result := range results {
		url := strings.TrimSpace(result.URL)
		if url == "" {
			continue
		}
		if _, found, err := c.directory.Document(ctx, url); err != nil || found {
			// Best-effort: on a lookup error skip rather than risk a blind write,
			// and when the URL is already stored keep the existing document so a
			// crawled full page is never replaced by a remote metadata stub.
			continue
		}
		if c.admission != nil && c.admission.CheckGrowth() != nil {
			return
		}
		doc := remoteResultDocument(result, c.now())
		receipt, err := c.receiver.Receive(ctx, []documentstore.Document{doc})
		if err != nil || receipt.Busy {
			continue
		}
		// Index into the full-text backend so the cached document is searchable; it
		// already lives in the store, so a failed index is only a soft miss.
		_ = c.index.Index(ctx, doc)
	}
}

// remoteResultDocument builds a metadata-only document from a remote search
// result. The snippet stands in for the extracted text so the document is
// findable by the words the peer already indexed; the store bounds the text.
func remoteResultDocument(result searchcore.Result, now time.Time) documentstore.Document {
	return documentstore.Document{
		CanonicalURL:  result.URL,
		NormalizedURL: result.URL,
		Title:         result.Title,
		ExtractedText: result.Snippet,
		Language:      result.Language,
		IndexedAt:     now,
	}
}

// remoteResults keeps only the results a peer supplied (Source == remote): local
// results are already in the index and web-fallback results are transient.
func remoteResults(results []searchcore.Result) []searchcore.Result {
	remote := make([]searchcore.Result, 0, len(results))
	for _, result := range results {
		if result.Source == searchcore.SourceRemote {
			remote = append(remote, result)
		}
	}

	return remote
}

// remoteCachingSearcher writes the metadata of remote results into the local
// index after each search, off the request path so it never adds latency. It is
// installed only when the operator leaves YAGO_INDEX_REMOTE_RESULTS on and the
// full-text index is available.
type remoteCachingSearcher struct {
	inner searchcore.Searcher
	store remoteResultStore
	spawn func(func()) bool
}

const (
	remoteCacheConcurrentWrites = 2
	remoteCacheWriteTimeout     = 30 * time.Second
)

func withRemoteIndexCache(
	inner searchcore.Searcher,
	store remoteResultStore,
) searchcore.Searcher {
	admission := newRemoteCacheWriteAdmission(remoteCacheConcurrentWrites)

	return remoteCachingSearcher{
		inner: inner,
		store: store,
		spawn: admission.try,
	}
}

func (s remoteCachingSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	resp, err := s.inner.Search(ctx, req)
	if err != nil {
		//nolint:wrapcheck // pass the wrapped searcher's error through unchanged.
		return resp, err
	}
	remote := remoteResults(resp.Results)
	if len(remote) > 0 {
		// Detach from the request context so the write finishes after the response
		// is served rather than being cancelled with it.
		cacheCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			remoteCacheWriteTimeout,
		)
		if !s.spawn(func() {
			defer cancel()
			s.store.store(cacheCtx, remote)
		}) {
			cancel()
		}
	}

	return resp, nil
}
