package yagonode

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type stubPrimarySearcher struct {
	resp searchcore.Response
}

func (s stubPrimarySearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	return s.resp, nil
}

type fallbackRoundTrip func(*http.Request) (*http.Response, error)

func (f fallbackRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const mojeekListFixture = `<ul><li><h2><a href="https://web.example/x">Hit about gap</a></h2><p>snippet</p></li></ul>`

func TestWithWebFallbackWrapsWhenConfigured(t *testing.T) {
	client := &http.Client{
		Transport: fallbackRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(mojeekListFixture)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	assembly := publicSearchAssembly{
		client: client,
		webFallback: webFallbackConfig{
			Enabled:  true,
			Privacy:  webFallbackPrivacyEnabled,
			Provider: webFallbackProviderDDGS,
			Backend:  "mojeek",
		},
	}

	search := withWebFallback(stubPrimarySearcher{}, assembly)
	resp, err := search.Search(context.Background(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Source != searchcore.SourceWeb {
		t.Fatalf("results = %#v", resp.Results)
	}
}

func TestWithWebFallbackInstallsSeeder(t *testing.T) {
	client := &http.Client{
		Transport: fallbackRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(mojeekListFixture)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	queue := &fakeCrawlQueue{}
	assembly := publicSearchAssembly{
		client:    client,
		storage:   nodeStorage{documentDirectory: fakeSeedDocuments{stored: map[string]bool{}}},
		seedQueue: queue,
		webFallback: webFallbackConfig{
			Enabled:      true,
			Privacy:      webFallbackPrivacyEnabled,
			Provider:     webFallbackProviderDDGS,
			Backend:      "mojeek",
			SeedCrawl:    true,
			SeedDepth:    1,
			SeedMaxPages: 20,
		},
	}

	search := withWebFallback(stubPrimarySearcher{}, assembly)
	if _, err := search.Search(
		context.Background(),
		searchcore.Request{Query: "gap", Limit: 10},
	); err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(queue.orders) != 1 || queue.keys[0] != "https://web.example/x" {
		t.Fatalf("seeded orders = %#v keys = %#v", queue.orders, queue.keys)
	}
}

func TestWithWebFallbackNoSeederWhenQueueMissing(t *testing.T) {
	assembly := publicSearchAssembly{
		webFallback: webFallbackConfig{
			Privacy:   webFallbackPrivacyEnabled,
			Provider:  webFallbackProviderDDGS,
			Backend:   "mojeek",
			SeedCrawl: true,
		},
	}

	if withWebFallback(stubPrimarySearcher{}, assembly) == nil {
		t.Fatal("expected a wrapped searcher even without a seed queue")
	}
}

func TestWithWebFallbackPassthroughWhenProviderUnset(t *testing.T) {
	assembly := publicSearchAssembly{webFallback: webFallbackConfig{Provider: ""}}

	search := withWebFallback(stubPrimarySearcher{}, assembly)
	resp, err := search.Search(context.Background(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Errorf("results = %#v, want empty passthrough", resp.Results)
	}
}

func fixtureFallbackClient() *http.Client {
	return &http.Client{
		Transport: fallbackRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(mojeekListFixture)),
				Header:     make(http.Header),
			}, nil
		}),
	}
}

func TestWithWebFallbackDisabledPrivacySkipsProvider(t *testing.T) {
	assembly := publicSearchAssembly{
		client: fixtureFallbackClient(),
		webFallback: webFallbackConfig{
			Enabled:  true,
			Privacy:  webFallbackPrivacyDisabled,
			Provider: webFallbackProviderDDGS,
			Backend:  "mojeek",
		},
	}

	search := withWebFallback(stubPrimarySearcher{}, assembly)
	resp, err := search.Search(context.Background(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("disabled privacy leaked web results: %#v", resp.Results)
	}
}

func TestWithWebFallbackExplicitRequiresOptIn(t *testing.T) {
	assembly := publicSearchAssembly{
		client: fixtureFallbackClient(),
		webFallback: webFallbackConfig{
			Privacy:  webFallbackPrivacyExplicit,
			Provider: webFallbackProviderDDGS,
			Backend:  "mojeek",
		},
	}
	search := withWebFallback(stubPrimarySearcher{}, assembly)

	resp, err := search.Search(context.Background(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil {
		t.Fatalf("search without opt-in: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("explicit mode fired without an opt-in: %#v", resp.Results)
	}

	optedIn := searchcore.Request{Query: "gap", Limit: 10, AllowWebFallback: true}
	resp, err = search.Search(context.Background(), optedIn)
	if err != nil {
		t.Fatalf("search with opt-in: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Source != searchcore.SourceWeb {
		t.Fatalf("explicit mode did not fire on opt-in: %#v", resp.Results)
	}
}

func TestWebFallbackPermit(t *testing.T) {
	if !webFallbackPermit(webFallbackPrivacyEnabled)(searchcore.Request{}) {
		t.Error("enabled mode must permit every request")
	}

	explicit := webFallbackPermit(webFallbackPrivacyExplicit)
	if explicit(searchcore.Request{}) {
		t.Error("explicit mode must reject a request that did not opt in")
	}
	if !explicit(searchcore.Request{AllowWebFallback: true}) {
		t.Error("explicit mode must permit a request that opted in")
	}
}
