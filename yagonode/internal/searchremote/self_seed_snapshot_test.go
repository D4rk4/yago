package searchremote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

func TestSelfSeedSnapshotResolvesOnceAcrossPeerWorkers(t *testing.T) {
	want := yagomodel.Seed{Name: yagomodel.Some("snapshot")}
	var calls atomic.Int32
	search := searcher{selfSeed: func(context.Context) yagomodel.Seed {
		calls.Add(1)

		return want
	}}.withSelfSeedSnapshot(t.Context())

	const workers = 32
	results := make(chan yagomodel.Seed, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- search.selfSeed(t.Context())
		}()
	}
	group.Wait()
	close(results)
	for result := range results {
		if name, ok := result.Name.Get(); !ok || name != "snapshot" {
			t.Fatalf("seed = %#v", result)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("self seed calls = %d", calls.Load())
	}
}

func TestSelfSeedSnapshotKeepsAbsentResolver(t *testing.T) {
	if searcher := (searcher{}).withSelfSeedSnapshot(t.Context()); searcher.selfSeed != nil {
		t.Fatal("nil self seed resolver changed")
	}
}

func TestSelfSeedSnapshotUsesTopLevelContext(t *testing.T) {
	want := yagomodel.Seed{Name: yagomodel.Some("snapshot")}
	search := searcher{selfSeed: func(ctx context.Context) yagomodel.Seed {
		if ctx.Err() != nil {
			return yagomodel.Seed{}
		}

		return want
	}}.withSelfSeedSnapshot(t.Context())
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	got := search.selfSeed(canceled)
	name, ok := got.Name.Get()
	if !ok || name != "snapshot" {
		t.Fatalf("seed = %#v, want %#v", got, want)
	}
}

func TestRemoteSearchReusesSelfSeedAcrossPeersAndMorphologyPasses(t *testing.T) {
	var formsMu sync.Mutex
	forms := make([]string, 0, 4)
	newPeer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			encoded := r.URL.Query().Get(yagoproto.FieldMySeed)
			if encoded == "" {
				t.Fatal("remote request has no self seed")
			}
			formsMu.Lock()
			forms = append(forms, encoded)
			formsMu.Unlock()
			writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
		}))
	}
	first := newPeer()
	defer first.Close()
	second := newPeer()
	defer second.Close()

	var resolverCalls atomic.Int32
	searcher := NewSearcher(Config{
		Client:      http.DefaultClient,
		NetworkName: "freeworld",
		Peers: fakePeerSource{peers: []yagomodel.Seed{
			serverSeed(t, first.URL),
			serverSeed(t, second.URL),
		}},
		MaxPeers:   2,
		Redundancy: 2,
		SelfSeed: func(context.Context) yagomodel.Seed {
			resolverCalls.Add(1)

			return searchSeed(t, "snapshot")
		},
		ExpandWord: func(string) []string { return []string{"drunklab", "drunklabs"} },
	})
	_, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "drunklab", Terms: []string{"drunklab"},
		Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	formsMu.Lock()
	gotForms := slices.Clone(forms)
	formsMu.Unlock()
	if resolverCalls.Load() != 1 || len(gotForms) != 4 {
		t.Fatalf("resolver calls = %d, remote forms = %d", resolverCalls.Load(), len(gotForms))
	}
	for _, encoded := range gotForms[1:] {
		if encoded != gotForms[0] {
			t.Fatalf("self seed forms = %q", gotForms)
		}
	}
}
