package yagonode

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestPublicSearchExplanationDoesNotSeedWebResults(t *testing.T) {
	queue := &fakeCrawlQueue{published: make(chan struct{}, 1)}
	assembly := publicSearchAssembly{
		client:    fixtureFallbackClient(),
		seedQueue: queue,
		storage: nodeStorage{
			documentDirectory: fakeSeedDocuments{stored: map[string]bool{}},
		},
		webFallback: webFallbackConfig{
			Enabled: true, Privacy: webFallbackPrivacyAlways,
			Provider: webFallbackProviderDDGS, Backend: "mojeek",
			SeedCrawl: true, SeedDepth: 1, SeedMaxPages: 20,
		},
	}
	searcher := assemblePublicExplanationSearcher(
		stubPrimarySearcher{}, stubPrimarySearcher{}, assembly,
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "gap", Source: searchcore.SourceGlobal, Limit: 10, Explain: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Source != searchcore.SourceWeb {
		t.Fatalf("response = %#v", response)
	}
	select {
	case <-queue.published:
		t.Fatalf("explanation seeded %d crawl orders", len(queue.orders))
	case <-time.After(100 * time.Millisecond):
	}
}
