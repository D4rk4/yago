package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestRemoteCacheWriteAdmissionBoundsConcurrentWork(t *testing.T) {
	admission := newRemoteCacheWriteAdmission(0)
	var queued func()
	admission.launch = func(work func()) { queued = work }

	if !admission.try(func() {}) {
		t.Fatal("first cache write was rejected")
	}
	if admission.try(func() {}) {
		t.Fatal("cache write was admitted while the only slot was occupied")
	}
	queued()
	if !admission.try(func() {}) {
		t.Fatal("cache write slot was not released")
	}
	queued()
}

type deadlineCacheStore struct {
	called      bool
	hasDeadline bool
}

func (s *deadlineCacheStore) store(ctx context.Context, _ []searchcore.Result) {
	s.called = true
	_, s.hasDeadline = ctx.Deadline()
}

func TestRemoteCachingSearcherBoundsDetachedWrites(t *testing.T) {
	response := searchcore.Response{Results: []searchcore.Result{{
		URL: "https://remote.example/", Source: searchcore.SourceRemote,
	}}}

	t.Run("deadline", func(t *testing.T) {
		store := &deadlineCacheStore{}
		searcher := remoteCachingSearcher{
			inner: &fakeSearcher{resp: response},
			store: store,
			spawn: func(work func()) bool {
				work()

				return true
			},
		}
		if _, err := searcher.Search(t.Context(), searchcore.Request{}); err != nil {
			t.Fatalf("Search: %v", err)
		}
		if !store.called || !store.hasDeadline {
			t.Fatalf("cache store called=%v deadline=%v", store.called, store.hasDeadline)
		}
	})

	t.Run("saturated", func(t *testing.T) {
		store := &deadlineCacheStore{}
		searcher := remoteCachingSearcher{
			inner: &fakeSearcher{resp: response},
			store: store,
			spawn: func(func()) bool { return false },
		}
		if _, err := searcher.Search(t.Context(), searchcore.Request{}); err != nil {
			t.Fatalf("Search: %v", err)
		}
		if store.called {
			t.Fatal("cache store ran after admission rejected it")
		}
	})
}
