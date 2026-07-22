package yagonode

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type fallbackPipelineEvents struct {
	mu     sync.Mutex
	values []string
}

func (e *fallbackPipelineEvents) add(value string) {
	e.mu.Lock()
	e.values = append(e.values, value)
	e.mu.Unlock()
}

func (e *fallbackPipelineEvents) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]string(nil), e.values...)
}

type fallbackPipelineSearcher struct {
	name         string
	events       *fallbackPipelineEvents
	exactResults []searchcore.Result
	fuzzyResults []searchcore.Result
	request      *searchcore.Request
}

func (s fallbackPipelineSearcher) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	results := s.exactResults
	event := s.name
	if req.Fuzzy {
		results = s.fuzzyResults
		event += "-fuzzy"
	} else if s.request != nil {
		*s.request = req
	}
	s.events.add(event)

	return searchcore.Response{
		Request: req, TotalResults: len(results), Results: results,
	}, nil
}

func fallbackPipelineAssembly(
	t *testing.T,
	events *fallbackPipelineEvents,
) (publicSearchAssembly, *atomic.Int32) {
	t.Helper()
	webCalls := &atomic.Int32{}
	client := &http.Client{
		Transport: fallbackRoundTrip(func(*http.Request) (*http.Response, error) {
			events.add("web")
			webCalls.Add(1)

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(mojeekListFixture)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	return publicSearchAssembly{
		client: client,
		webFallback: webFallbackConfig{
			Privacy: webFallbackPrivacyEnabled, Provider: webFallbackProviderDDGS,
			Backend: "mojeek", MaxResults: 10, Timeout: time.Second,
		},
	}, webCalls
}

func TestPublicSearchFallsBackAfterLocalFuzzyMiss(t *testing.T) {
	events := &fallbackPipelineEvents{}
	assembly, webCalls := fallbackPipelineAssembly(t, events)
	searcher := assemblePublicSearcher(
		fallbackPipelineSearcher{name: "local", events: events},
		fallbackPipelineSearcher{name: "swarm", events: events},
		assembly,
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "gap", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Source != searchcore.SourceWeb ||
		webCalls.Load() != 1 {
		t.Fatalf("response = %#v web calls = %d", response, webCalls.Load())
	}
	sequence := events.snapshot()
	fuzzy := eventPosition(sequence, "local-fuzzy")
	web := eventPosition(sequence, "web")
	local := eventPosition(sequence, "local")
	swarm := eventPosition(sequence, "swarm")
	if local < 0 || swarm < 0 || fuzzy <= local || fuzzy <= swarm || web <= fuzzy {
		t.Fatalf("stage order = %v", sequence)
	}
}

func TestPublicSearchKeepsLocalFuzzyRecoveryBeforeWeb(t *testing.T) {
	events := &fallbackPipelineEvents{}
	assembly, webCalls := fallbackPipelineAssembly(t, events)
	searcher := assemblePublicSearcher(
		fallbackPipelineSearcher{
			name: "local", events: events,
			fuzzyResults: []searchcore.Result{{
				Title: "Recovered gap", URL: "https://local.example/gap",
				Source: searchcore.SourceLocal,
			}},
		},
		fallbackPipelineSearcher{name: "swarm", events: events},
		assembly,
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "gap", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Recovered != "fuzzy" || webCalls.Load() != 0 {
		t.Fatalf("response = %#v web calls = %d", response, webCalls.Load())
	}
}

func TestPublicSearchParallelModeCombinesFuzzyAndWebAnswers(t *testing.T) {
	events := &fallbackPipelineEvents{}
	assembly, webCalls := fallbackPipelineAssembly(t, events)
	assembly.webFallback.Privacy = webFallbackPrivacyAlways
	searcher := assemblePublicSearcher(
		fallbackPipelineSearcher{
			name: "local", events: events,
			fuzzyResults: []searchcore.Result{{
				Title: "Recovered gap", URL: "https://local.example/gap",
				Source: searchcore.SourceLocal,
			}},
		},
		fallbackPipelineSearcher{name: "swarm", events: events},
		assembly,
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "gap", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 2 || response.Recovered != "fuzzy" || webCalls.Load() != 1 {
		t.Fatalf("response = %#v web calls = %d", response, webCalls.Load())
	}
	sources := map[searchcore.Source]bool{}
	for _, result := range response.Results {
		sources[result.Source] = true
	}
	if !sources[searchcore.SourceLocal] || !sources[searchcore.SourceWeb] {
		t.Fatalf("sources = %#v", sources)
	}
}

func TestPublicSearchKeepsSwarmHitBeforeRecoveryAndWeb(t *testing.T) {
	events := &fallbackPipelineEvents{}
	assembly, webCalls := fallbackPipelineAssembly(t, events)
	searcher := assemblePublicSearcher(
		fallbackPipelineSearcher{name: "local", events: events},
		fallbackPipelineSearcher{
			name: "swarm", events: events,
			exactResults: []searchcore.Result{{
				Title: "Swarm gap", URL: "https://peer.example/gap",
				Source: searchcore.SourceRemote,
			}},
		},
		assembly,
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "gap", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	sequence := events.snapshot()
	if len(response.Results) != 1 || webCalls.Load() != 0 ||
		eventPosition(sequence, "local-fuzzy") >= 0 || eventPosition(sequence, "web") >= 0 {
		t.Fatalf("response = %#v web calls = %d events = %v", response, webCalls.Load(), sequence)
	}
}

func TestPublicSearchDropsEvidenceFreeSwarmHitBeforeWebFallback(t *testing.T) {
	events := &fallbackPipelineEvents{}
	assembly, webCalls := fallbackPipelineAssembly(t, events)
	searcher := assemblePublicSearcher(
		fallbackPipelineSearcher{name: "local", events: events},
		fallbackPipelineSearcher{
			name: "swarm", events: events,
			exactResults: []searchcore.Result{{
				Title: "Unrelated page", URL: "https://peer.example/other",
				Source: searchcore.SourceRemote,
			}},
		},
		assembly,
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "gap", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	sequence := events.snapshot()
	if len(response.Results) != 1 || response.Results[0].Source != searchcore.SourceWeb ||
		webCalls.Load() != 1 || eventPosition(sequence, "local-fuzzy") < 0 ||
		eventPosition(sequence, "web") < 0 {
		t.Fatalf("response = %#v web calls = %d events = %v", response, webCalls.Load(), sequence)
	}
}

func TestPublicSearchDropsEvidenceFreeSwarmHitBeforeLocalFuzzyRecovery(t *testing.T) {
	events := &fallbackPipelineEvents{}
	assembly, webCalls := fallbackPipelineAssembly(t, events)
	searcher := assemblePublicSearcher(
		fallbackPipelineSearcher{
			name: "local", events: events,
			fuzzyResults: []searchcore.Result{{
				Title: "Recovered gap", URL: "https://local.example/gap",
				Source: searchcore.SourceLocal,
			}},
		},
		fallbackPipelineSearcher{
			name: "swarm", events: events,
			exactResults: []searchcore.Result{{
				Title: "Unrelated page", URL: "https://peer.example/other",
				Source: searchcore.SourceRemote,
			}},
		},
		assembly,
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "gap", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Source != searchcore.SourceLocal ||
		response.Recovered != "fuzzy" || webCalls.Load() != 0 {
		t.Fatalf("response = %#v web calls = %d events = %v",
			response, webCalls.Load(), events.snapshot())
	}
}

func TestPublicSearchAcceptsVisibleSwarmMorphologyWithoutRecovery(t *testing.T) {
	events := &fallbackPipelineEvents{}
	assembly, webCalls := fallbackPipelineAssembly(t, events)
	searcher := assemblePublicSearcher(
		fallbackPipelineSearcher{name: "local", events: events},
		fallbackPipelineSearcher{
			name: "swarm", events: events,
			exactResults: []searchcore.Result{{
				Title: "Записки о псилобатах", URL: "https://peer.example/history",
				Source: searchcore.SourceRemote, Language: "en",
			}},
		},
		assembly,
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "псилобаты", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	sequence := events.snapshot()
	if len(response.Results) != 1 || response.Results[0].Source != searchcore.SourceRemote ||
		webCalls.Load() != 0 || eventPosition(sequence, "local-fuzzy") >= 0 ||
		eventPosition(sequence, "web") >= 0 {
		t.Fatalf("response = %#v web calls = %d events = %v", response, webCalls.Load(), sequence)
	}
}

func TestPublicSearchLocalSourceNeverUsesSwarmOrWeb(t *testing.T) {
	events := &fallbackPipelineEvents{}
	assembly, webCalls := fallbackPipelineAssembly(t, events)
	searcher := assemblePublicSearcher(
		fallbackPipelineSearcher{name: "local", events: events},
		fallbackPipelineSearcher{name: "swarm", events: events},
		assembly,
	)

	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "gap", Source: searchcore.SourceLocal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	sequence := events.snapshot()
	if len(response.Results) != 0 || webCalls.Load() != 0 ||
		eventPosition(sequence, "swarm") >= 0 || eventPosition(sequence, "web") >= 0 ||
		eventPosition(sequence, "local") < 0 || eventPosition(sequence, "local-fuzzy") < 0 {
		t.Fatalf("response = %#v web calls = %d events = %v", response, webCalls.Load(), sequence)
	}
}

func TestPublicSearchPreservesStructuredQueryForWebFallback(t *testing.T) {
	events := &fallbackPipelineEvents{}
	submitted := `site:example.org filetype:pdf "golang tools" -java`
	providerQueries := make(chan string, 1)
	client := &http.Client{
		Transport: fallbackRoundTrip(func(req *http.Request) (*http.Response, error) {
			providerQueries <- req.URL.Query().Get("q")

			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`<ul>
<li><h2><a href="https://www.example.org/guides/golang-tools.pdf">Golang tools handbook</a></h2><p>Reference</p></li>
<li><h2><a href="https://other.test/golang-tools.pdf">Golang tools mirror</a></h2><p>Reference</p></li>
<li><h2><a href="https://www.example.org/guides/golang-tools.html">Golang tools page</a></h2><p>Reference</p></li>
<li><h2><a href="https://www.example.org/guides/java-tools.pdf">Golang and Java tools</a></h2><p>Reference</p></li>
</ul>`)),
				Header: make(http.Header),
			}, nil
		}),
	}
	assembly := publicSearchAssembly{
		client: client,
		webFallback: webFallbackConfig{
			Privacy: webFallbackPrivacyEnabled, Provider: webFallbackProviderDDGS,
			Backend: "mojeek", MaxResults: 10, Timeout: time.Second,
		},
	}
	var localRequest, swarmRequest searchcore.Request
	searcher := assemblePublicSearcher(
		fallbackPipelineSearcher{name: "local", events: events, request: &localRequest},
		fallbackPipelineSearcher{name: "swarm", events: events, request: &swarmRequest},
		assembly,
	)

	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: submitted, Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	providerQuery := <-providerQueries
	if providerQuery != submitted {
		t.Fatalf("provider query = %q", providerQuery)
	}
	for name, request := range map[string]searchcore.Request{
		"local": localRequest, "swarm": swarmRequest,
	} {
		if request.Query != "golang tools" || request.SubmittedQuery != submitted {
			t.Fatalf("%s request = %#v", name, request)
		}
	}
	if len(response.Results) != 1 ||
		response.Results[0].URL != "https://www.example.org/guides/golang-tools.pdf" {
		t.Fatalf("results = %#v", response.Results)
	}
}

func eventPosition(events []string, sought string) int {
	for position, event := range events {
		if event == sought {
			return position
		}
	}

	return -1
}
