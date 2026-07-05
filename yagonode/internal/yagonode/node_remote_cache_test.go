package yagonode

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func newRoundTripCache(
	t *testing.T,
) (remoteIndexCache, *searchindex.BleveMemoryIndex, documentstore.DocumentDirectory) {
	t.Helper()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	directory, receiver, err := documentstore.Open(v)
	if err != nil {
		t.Fatalf("documentstore.Open: %v", err)
	}
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	cache := remoteIndexCache{
		directory: directory,
		receiver:  receiver,
		index:     index,
		now:       func() time.Time { return time.Unix(1_700_000_000, 0) },
	}

	return cache, index, directory
}

func TestRemoteIndexCacheStoresAndFindsRemoteResult(t *testing.T) {
	cache, index, directory := newRoundTripCache(t)

	cache.store(t.Context(), []searchcore.Result{{
		URL:     "https://peer.example.net/report",
		Title:   "Report",
		Snippet: "Zelensky addressed the nation.",
		Source:  searchcore.SourceRemote,
	}})

	results, err := index.Search(
		t.Context(),
		searchindex.SearchRequest{Query: "nation", MaxResults: 5},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results.Total != 1 {
		t.Fatalf("Total = %d, want the cached remote result to be findable locally", results.Total)
	}
	doc, found, err := directory.Document(t.Context(), "https://peer.example.net/report")
	if err != nil || !found {
		t.Fatalf("Document found=%v err=%v, want the cached document in the store", found, err)
	}
	if doc.Title != "Report" {
		t.Fatalf("cached Title = %q, want %q", doc.Title, "Report")
	}
}

func TestRemoteIndexCacheDoesNotOverwriteStoredDocument(t *testing.T) {
	cache, _, directory := newRoundTripCache(t)
	const url = "https://peer.example.net/page"

	if _, err := cache.receiver.Receive(t.Context(), []documentstore.Document{{
		NormalizedURL: url,
		Title:         "Crawled",
		ExtractedText: "The full crawled body.",
	}}); err != nil {
		t.Fatalf("seed crawled document: %v", err)
	}

	cache.store(t.Context(), []searchcore.Result{{
		URL:     url,
		Title:   "Remote stub",
		Snippet: "A lighter remote snippet.",
		Source:  searchcore.SourceRemote,
	}})

	doc, found, err := directory.Document(t.Context(), url)
	if err != nil || !found {
		t.Fatalf("Document found=%v err=%v", found, err)
	}
	if doc.Title != "Crawled" {
		t.Fatalf(
			"Title = %q, want the crawled document preserved, not replaced by the stub",
			doc.Title,
		)
	}
}

func TestRemoteIndexCacheSkipsBlankURL(t *testing.T) {
	cache, index, _ := newRoundTripCache(t)

	cache.store(t.Context(), []searchcore.Result{{
		URL:    "   ",
		Title:  "No URL",
		Source: searchcore.SourceRemote,
	}})

	stats, err := index.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Documents != 0 {
		t.Fatalf("Documents = %d, want a blank URL to be skipped", stats.Documents)
	}
}

// fakeDocDirectory reports a fixed lookup outcome so the store's skip branches
// (lookup error, already present) can be exercised without a real vault.
type fakeDocDirectory struct {
	found bool
	err   error
}

func (f fakeDocDirectory) Document(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	return documentstore.Document{}, f.found, f.err
}

func (fakeDocDirectory) Count(context.Context) (int, error) { return 0, nil }

type fakeDocReceiver struct {
	err      error
	received []documentstore.Document
}

func (f *fakeDocReceiver) Receive(
	_ context.Context,
	docs []documentstore.Document,
) (documentstore.Receipt, error) {
	f.received = append(f.received, docs...)

	return documentstore.Receipt{}, f.err
}

type recordingIndex struct {
	indexed []documentstore.Document
}

func (r *recordingIndex) Index(_ context.Context, doc documentstore.Document) error {
	r.indexed = append(r.indexed, doc)

	return nil
}

func (recordingIndex) Delete(context.Context, string) error { return nil }

func (recordingIndex) Search(
	context.Context,
	searchindex.SearchRequest,
) (searchindex.SearchResultSet, error) {
	return searchindex.SearchResultSet{}, nil
}

func (recordingIndex) Stats(context.Context) (searchindex.IndexStats, error) {
	return searchindex.IndexStats{}, nil
}

func TestRemoteIndexCacheSkipsOnDirectoryError(t *testing.T) {
	index := &recordingIndex{}
	cache := remoteIndexCache{
		directory: fakeDocDirectory{err: errors.New("lookup failed")},
		receiver:  &fakeDocReceiver{},
		index:     index,
		now:       func() time.Time { return time.Unix(1, 0) },
	}

	cache.store(t.Context(), []searchcore.Result{{
		URL:    "https://peer.example.net/x",
		Source: searchcore.SourceRemote,
	}})

	if len(index.indexed) != 0 {
		t.Fatalf("indexed %d documents, want none after a lookup error", len(index.indexed))
	}
}

func TestRemoteIndexCacheSkipsOnReceiveError(t *testing.T) {
	receiver := &fakeDocReceiver{err: errors.New("store full")}
	index := &recordingIndex{}
	cache := remoteIndexCache{
		directory: fakeDocDirectory{found: false},
		receiver:  receiver,
		index:     index,
		now:       func() time.Time { return time.Unix(1, 0) },
	}

	cache.store(t.Context(), []searchcore.Result{{
		URL:    "https://peer.example.net/y",
		Source: searchcore.SourceRemote,
	}})

	if len(receiver.received) != 1 {
		t.Fatalf("receiver saw %d documents, want the one write attempt", len(receiver.received))
	}
	if len(index.indexed) != 0 {
		t.Fatalf("indexed %d documents, want none when the store write failed", len(index.indexed))
	}
}

func TestRemoteResultsKeepsOnlyRemote(t *testing.T) {
	remote := remoteResults([]searchcore.Result{
		{URL: "https://a", Source: searchcore.SourceLocal},
		{URL: "https://b", Source: searchcore.SourceRemote},
		{URL: "https://c", Source: searchcore.SourceWeb},
		{URL: "https://d", Source: searchcore.SourceRemote},
	})

	if len(remote) != 2 || remote[0].URL != "https://b" || remote[1].URL != "https://d" {
		t.Fatalf("remoteResults = %#v, want only the two remote results", remote)
	}
}

// recordingCacheStore captures the batches the caching decorator hands off so the
// spawn wiring can be asserted; done, when set, signals an asynchronous store.
type recordingCacheStore struct {
	mu      sync.Mutex
	batches [][]searchcore.Result
	done    chan struct{}
}

func (r *recordingCacheStore) store(_ context.Context, results []searchcore.Result) {
	r.mu.Lock()
	r.batches = append(r.batches, results)
	r.mu.Unlock()
	if r.done != nil {
		close(r.done)
	}
}

func (r *recordingCacheStore) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.batches)
}

func TestRemoteCachingSearcherPassesInnerError(t *testing.T) {
	store := &recordingCacheStore{}
	searcher := remoteCachingSearcher{
		inner: &fakeSearcher{err: errors.New("boom")},
		store: store,
		spawn: func(work func()) { work() },
	}

	if _, err := searcher.Search(t.Context(), searchcore.Request{}); err == nil {
		t.Fatal("Search error = nil, want the inner searcher error passed through")
	}
	if store.count() != 0 {
		t.Fatalf("stored %d batches, want none on a failed search", store.count())
	}
}

func TestRemoteCachingSearcherStoresRemoteResults(t *testing.T) {
	store := &recordingCacheStore{}
	searcher := remoteCachingSearcher{
		inner: &fakeSearcher{resp: searchcore.Response{Results: []searchcore.Result{
			{URL: "https://local", Source: searchcore.SourceLocal},
			{URL: "https://remote", Source: searchcore.SourceRemote},
		}}},
		store: store,
		spawn: func(work func()) { work() },
	}

	if _, err := searcher.Search(t.Context(), searchcore.Request{}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if store.count() != 1 {
		t.Fatalf("stored %d batches, want one", store.count())
	}
	if got := store.batches[0]; len(got) != 1 || got[0].URL != "https://remote" {
		t.Fatalf("stored batch = %#v, want only the remote result", got)
	}
}

func TestRemoteCachingSearcherIgnoresWhenNoRemote(t *testing.T) {
	store := &recordingCacheStore{}
	searcher := remoteCachingSearcher{
		inner: &fakeSearcher{resp: searchcore.Response{Results: []searchcore.Result{
			{URL: "https://local", Source: searchcore.SourceLocal},
		}}},
		store: store,
		spawn: func(work func()) { work() },
	}

	if _, err := searcher.Search(t.Context(), searchcore.Request{}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if store.count() != 0 {
		t.Fatalf("stored %d batches, want none when there are no remote results", store.count())
	}
}

func TestWithRemoteIndexCacheSpawnsStoreAsynchronously(t *testing.T) {
	store := &recordingCacheStore{done: make(chan struct{})}
	searcher := withRemoteIndexCache(
		&fakeSearcher{resp: searchcore.Response{Results: []searchcore.Result{
			{URL: "https://remote", Source: searchcore.SourceRemote},
		}}},
		store,
	)

	if _, err := searcher.Search(t.Context(), searchcore.Request{}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	select {
	case <-store.done:
	case <-time.After(2 * time.Second):
		t.Fatal("store was not called; the decorator did not spawn the cache write")
	}
	if store.count() != 1 {
		t.Fatalf("stored %d batches, want one", store.count())
	}
}
