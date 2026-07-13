package yagonode

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/snippetfetch"
)

type productionShapeLocalMiss struct{}

func (productionShapeLocalMiss) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if req.Fuzzy {
		<-ctx.Done()
	}

	return searchcore.Response{Request: req}, nil
}

type productionShapeLocalHit struct{}

func (productionShapeLocalHit) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	return searchcore.Response{
		Request: req,
		Results: []searchcore.Result{{
			Title:  "Needle term local",
			URL:    "https://local.example/needle",
			Source: searchcore.SourceLocal,
		}},
		TotalResults: 1,
	}, nil
}

type productionShapeUncooperativeFuzzy struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (s productionShapeUncooperativeFuzzy) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if !req.Fuzzy {
		return searchcore.Response{Request: req}, nil
	}
	close(s.started)
	<-s.release
	close(s.finished)

	return searchcore.Response{Request: req}, nil
}

type productionShapeSwarmMiss struct{}

func (productionShapeSwarmMiss) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	return searchcore.Response{Request: req}, nil
}

type productionShapeEvidenceFreeSwarm struct{}

type productionShapeUncooperativeSwarm struct {
	release  <-chan struct{}
	finished chan<- struct{}
	calls    atomic.Int32
}

func (s *productionShapeUncooperativeSwarm) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	s.calls.Add(1)
	<-s.release
	s.finished <- struct{}{}

	return searchcore.Response{}, nil
}

type productionShapeWebAttempts struct {
	mu      sync.Mutex
	hosts   []string
	queries []string
}

func (productionShapeEvidenceFreeSwarm) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	timer := time.NewTimer(30 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return searchcore.Response{Request: req}, fmt.Errorf("slow swarm: %w", ctx.Err())
	case <-timer.C:
	}

	return searchcore.Response{
		Request: req,
		Results: []searchcore.Result{{
			Title:  "Unrelated peer row",
			URL:    "https://peer.example/unrelated",
			Source: searchcore.SourceRemote,
		}},
		TotalResults: 1,
	}, nil
}

func (attempts *productionShapeWebAttempts) roundTrip(
	request *http.Request,
) (*http.Response, error) {
	attempts.mu.Lock()
	attempts.hosts = append(attempts.hosts, request.URL.Host)
	attempts.queries = append(attempts.queries, request.URL.Query().Get("q"))
	attempts.mu.Unlock()

	delay := 10 * time.Second
	status := http.StatusOK
	body := ""
	switch request.URL.Host {
	case "html.duckduckgo.com":
		delay = 229 * time.Millisecond
		status = http.StatusAccepted
	case "lite.duckduckgo.com":
		delay = 795 * time.Millisecond
		body = `<table>
<tr><td><a class="result-link" href="https://docs.example.org/needle.pdf">Needle term guide</a></td></tr>
<tr><td class="result-snippet">Needle term reference</td></tr>
</table>`
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-request.Context().Done():
		return nil, fmt.Errorf("provider canceled: %w", request.Context().Err())
	case <-timer.C:
	}

	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func (attempts *productionShapeWebAttempts) snapshot() ([]string, []string) {
	attempts.mu.Lock()
	defer attempts.mu.Unlock()

	return slices.Clone(attempts.hosts), slices.Clone(attempts.queries)
}

func productionShapeSearchAssembly(client *http.Client) publicSearchAssembly {
	return publicSearchAssembly{
		client: client,
		snippetEnricher: snippetfetch.NewEnricher(func(
			ctx context.Context,
			_ string,
		) (string, error) {
			<-ctx.Done()

			return "", fmt.Errorf("evidence fetch: %w", ctx.Err())
		}),
		webFallback: webFallbackConfig{
			Privacy:  webFallbackPrivacyEnabled,
			Provider: webFallbackProviderDDGS,
			Backend:  "auto",
			Timeout:  time.Second,
		},
	}
}

func TestPublicSearchReservesUsableWebWindowAfterSlowSwarmAndFuzzyMiss(t *testing.T) {
	previousExact := webFallbackExactStageBudget
	previousRecovery := recoverySearchBudget
	webFallbackExactStageBudget = 60 * time.Millisecond
	recoverySearchBudget = 15 * time.Millisecond
	t.Cleanup(func() {
		webFallbackExactStageBudget = previousExact
		recoverySearchBudget = previousRecovery
	})

	attempts := &productionShapeWebAttempts{}
	client := &http.Client{Transport: fallbackRoundTrip(attempts.roundTrip)}
	searcher := assemblePublicSearcher(
		productionShapeLocalMiss{},
		productionShapeEvidenceFreeSwarm{},
		productionShapeSearchAssembly(client),
	)

	submitted := `site:example.org "needle term"`
	started := time.Now()
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: submitted, Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("elapsed = %v", elapsed)
	}
	hosts, queries := attempts.snapshot()
	if !slices.Contains(hosts, "html.duckduckgo.com") ||
		!slices.Contains(hosts, "lite.duckduckgo.com") {
		t.Fatalf("provider hosts = %v", hosts)
	}
	for _, query := range queries {
		if query != submitted {
			t.Fatalf("provider queries = %q", queries)
		}
	}
	if len(response.Results) != 1 ||
		response.Results[0].Source != searchcore.SourceWeb ||
		response.Results[0].URL != "https://docs.example.org/needle.pdf" {
		t.Fatalf("response = %#v", response)
	}
}

func TestPublicSearchParallelModeIncludesWebBesidePrimaryHit(t *testing.T) {
	previousExact := webFallbackExactStageBudget
	webFallbackExactStageBudget = 60 * time.Millisecond
	t.Cleanup(func() { webFallbackExactStageBudget = previousExact })

	attempts := &productionShapeWebAttempts{}
	client := &http.Client{Transport: fallbackRoundTrip(attempts.roundTrip)}
	assembly := productionShapeSearchAssembly(client)
	assembly.webFallback.Privacy = webFallbackPrivacyAlways
	searcher := assemblePublicSearcher(
		productionShapeLocalHit{},
		productionShapeEvidenceFreeSwarm{},
		assembly,
	)

	started := time.Now()
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "needle term", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 1500*time.Millisecond {
		t.Fatalf("parallel search exceeded the response budget")
	}
	sources := map[searchcore.Source]bool{}
	for _, result := range response.Results {
		sources[result.Source] = true
	}
	if !sources[searchcore.SourceLocal] || !sources[searchcore.SourceWeb] {
		t.Fatalf("response = %#v", response)
	}
	hosts, _ := attempts.snapshot()
	if !slices.Contains(hosts, "lite.duckduckgo.com") {
		t.Fatalf("provider hosts = %v", hosts)
	}
}

func TestPublicSearchParallelModeReturnsWebWhileFuzzyIgnoresCancellation(t *testing.T) {
	previousRecovery := recoverySearchBudget
	recoverySearchBudget = 20 * time.Millisecond
	t.Cleanup(func() { recoverySearchBudget = previousRecovery })

	local := productionShapeUncooperativeFuzzy{
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
	attempts := &productionShapeWebAttempts{}
	client := &http.Client{Transport: fallbackRoundTrip(attempts.roundTrip)}
	assembly := productionShapeSearchAssembly(client)
	assembly.webFallback.Privacy = webFallbackPrivacyAlways
	searcher := assemblePublicSearcher(local, productionShapeSwarmMiss{}, assembly)

	started := time.Now()
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "needle term", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 1500*time.Millisecond || len(response.Results) != 1 ||
		response.Results[0].Source != searchcore.SourceWeb {
		t.Fatalf("response = %#v", response)
	}
	select {
	case <-local.started:
	default:
		t.Fatal("fuzzy search did not start")
	}
	select {
	case <-local.finished:
		t.Fatal("fuzzy search finished before release")
	default:
	}
	close(local.release)
	select {
	case <-local.finished:
	case <-time.After(time.Second):
		t.Fatal("fuzzy search did not finish")
	}
}

func TestPublicSearchStageBudgetsLeaveAssemblyHeadroom(t *testing.T) {
	stages := webFallbackExactStageBudget +
		max(recoverySearchBudget, localExactRecoveryBudget) +
		webFallbackProviderBudget
	if webFallbackProviderBudget < 900*time.Millisecond {
		t.Fatalf("web budget = %v", webFallbackProviderBudget)
	}
	if headroom := interactiveSearchBudget - stages; headroom < 100*time.Millisecond {
		t.Fatalf("stage total = %v, headroom = %v", stages, headroom)
	}
}

func TestExactStageDeadlinePreservesCompletedLocalResults(t *testing.T) {
	previous := webFallbackExactStageBudget
	webFallbackExactStageBudget = 20 * time.Millisecond
	t.Cleanup(func() { webFallbackExactStageBudget = previous })

	searcher := withWebFallbackExactStageBudget(
		searchcore.NewFederatedSearcher(
			productionShapeLocalHit{},
			productionShapeEvidenceFreeSwarm{},
		),
		webFallbackConfig{
			Privacy:  webFallbackPrivacyEnabled,
			Provider: webFallbackProviderDDGS,
		},
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "needle term", Source: searchcore.SourceGlobal, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 ||
		response.Results[0].URL != "https://local.example/needle" {
		t.Fatalf("response = %#v", response)
	}
}

func TestRepeatedGlobalSearchKeepsLocalHitWhileSwarmIgnoresCancellation(t *testing.T) {
	previous := webFallbackExactStageBudget
	previousRemoteAdmission := processRemoteSearchAdmission
	webFallbackExactStageBudget = 20 * time.Millisecond
	processRemoteSearchAdmission = make(chan struct{}, interactiveSearchConcurrentWork)
	t.Cleanup(func() {
		webFallbackExactStageBudget = previous
		processRemoteSearchAdmission = previousRemoteAdmission
	})

	release := make(chan struct{})
	finished := make(chan struct{}, 6)
	remote := &productionShapeUncooperativeSwarm{release: release, finished: finished}
	attempts := &productionShapeWebAttempts{}
	client := &http.Client{Transport: fallbackRoundTrip(attempts.roundTrip)}
	searcher := assemblePublicSearcher(
		productionShapeLocalHit{},
		remote,
		productionShapeSearchAssembly(client),
	)
	for attempt := range 6 {
		response, err := searcher.Search(t.Context(), searchcore.Request{
			Query: "drunklab", Source: searchcore.SourceGlobal, Limit: 10,
		})
		if err != nil || len(response.Results) == 0 ||
			response.Results[0].URL != "https://local.example/needle" {
			t.Fatalf("attempt %d response = %#v, error = %v", attempt+1, response, err)
		}
	}
	if hosts, _ := attempts.snapshot(); len(hosts) != 0 {
		t.Fatalf("provider hosts = %v", hosts)
	}
	if remote.calls.Load() != interactiveSearchConcurrentWork {
		t.Fatalf("remote calls = %d", remote.calls.Load())
	}
	close(release)
	for range interactiveSearchConcurrentWork {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("remote search did not finish")
		}
	}
}
