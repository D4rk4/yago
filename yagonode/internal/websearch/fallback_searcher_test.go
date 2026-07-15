package websearch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type stubSearcher struct {
	resp  searchcore.Response
	err   error
	calls int
}

func (s *stubSearcher) Search(context.Context, searchcore.Request) (searchcore.Response, error) {
	s.calls++

	return s.resp, s.err
}

type stubProvider struct {
	results  []Result
	err      error
	calls    int
	gotQuery string
	gotLimit int
}

func (p *stubProvider) Search(_ context.Context, query string, limit int) ([]Result, error) {
	p.calls++
	p.gotQuery = query
	p.gotLimit = limit

	return p.results, p.err
}

func enabled(searchcore.Request) bool  { return true }
func disabled(searchcore.Request) bool { return false }

func TestFallbackPermitGatesOnRequest(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{results: []Result{{Title: "web", URL: "https://a.example.com/x"}}}
	permit := func(req searchcore.Request) bool { return req.AllowWebFallback }
	searcher := NewFallbackSearcher(primary, provider, permit)

	if _, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: "q", Limit: 10},
	); err != nil {
		t.Fatalf("search without opt-in: %v", err)
	}
	if provider.calls != 0 {
		t.Fatal("provider ran without a per-request opt-in")
	}

	optedIn := searchcore.Request{Query: "q", Limit: 10, AllowWebFallback: true}
	if _, err := searcher.Search(context.Background(), optedIn); err != nil {
		t.Fatalf("search with opt-in: %v", err)
	}
	if provider.calls != 1 {
		t.Fatal("provider did not run with a per-request opt-in")
	}
}

func TestFallbackSkippedWhenPrimaryHasResults(t *testing.T) {
	primary := &stubSearcher{
		resp: searchcore.Response{Results: []searchcore.Result{{Title: "owned"}}},
	}
	provider := &stubProvider{results: []Result{{Title: "web"}}}
	searcher := NewFallbackSearcher(primary, provider, enabled)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "x", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if provider.calls != 0 {
		t.Error("provider must not run when the primary has results")
	}
	if len(resp.Results) != 1 || resp.Results[0].Title != "owned" {
		t.Errorf("results = %#v", resp.Results)
	}
}

func TestFallbackRunsOnMiss(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{results: []Result{
		{Title: "web one gap", URL: "https://a.example.com/x", Snippet: "s1"},
		{Title: "web two", URL: "https://b.example.com/y", Snippet: "the gap explained"},
	}}
	searcher := NewFallbackSearcher(primary, provider, enabled)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if provider.calls != 1 || provider.gotQuery != "gap" || provider.gotLimit != 10 {
		t.Fatalf("provider call = %#v", provider)
	}
	if len(resp.Results) != 2 || resp.TotalResults != 2 {
		t.Fatalf("results = %#v total=%d", resp.Results, resp.TotalResults)
	}
	first := resp.Results[0]
	if first.Source != searchcore.SourceWeb || first.Host != "a.example.com" {
		t.Errorf("first result = %#v", first)
	}
	if resp.Results[0].Score <= resp.Results[1].Score {
		t.Errorf("scores should decay: %v %v", resp.Results[0].Score, resp.Results[1].Score)
	}
}

func TestFallbackSkippedWhenDisabled(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{results: []Result{{Title: "web"}}}
	searcher := NewFallbackSearcher(primary, provider, disabled)

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if provider.calls != 0 {
		t.Error("provider must not run when disabled")
	}
	if len(resp.Results) != 0 {
		t.Errorf("results = %#v", resp.Results)
	}
}

func TestFallbackSkipsLocalSourceWithoutConsent(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{results: []Result{{Title: "web gap"}}}
	searcher := NewFallbackSearcher(primary, provider, enabled)

	resp, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "gap", Source: searchcore.SourceLocal, Limit: 10,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if provider.calls != 0 {
		t.Fatal("provider ran for a local-only request")
	}
	if len(resp.Results) != 0 {
		t.Fatalf("local-only results = %#v", resp.Results)
	}
}

func TestFallbackRunsForConsentingLocalRetrieval(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{results: []Result{{Title: "web gap"}}}
	searcher := NewFallbackSearcher(primary, provider, enabled)

	resp, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "gap", Source: searchcore.SourceLocal, Limit: 10, AllowWebFallback: true,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if provider.calls != 1 || len(resp.Results) != 1 {
		t.Fatalf("provider calls = %d, results = %#v", provider.calls, resp.Results)
	}
}

func TestFallbackUsesSubmittedQuery(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{results: []Result{{
		Title: "Golang tools", URL: "https://example.org/golang-tools.pdf",
	}}}
	searcher := NewFallbackSearcher(primary, provider, enabled)
	submitted := `site:example.org filetype:pdf "golang tools" -java`

	resp, err := searcher.Search(context.Background(), searchcore.Request{
		Query: "golang tools", SubmittedQuery: submitted,
		Terms: []string{"golang", "tools"}, SiteHost: "example.org",
		FileType: "pdf", ExcludedTerms: []string{"java"}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.gotQuery != submitted || len(resp.Results) != 1 {
		t.Fatalf("provider query = %q, results = %#v", provider.gotQuery, resp.Results)
	}
}

func TestFallbackSkippedWhenQueryBlank(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{results: []Result{{Title: "web"}}}
	searcher := NewFallbackSearcher(primary, provider, enabled)

	if _, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: "  ", Limit: 10},
	); err != nil {
		t.Fatalf("search: %v", err)
	}
	if provider.calls != 0 {
		t.Error("provider must not run for a blank query")
	}
}

func TestFallbackDegradesOnProviderError(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{
		results: []Result{{Title: "Unrelated", URL: "https://web.example/other"}},
		err:     errors.New("boom"),
	}
	seeder := &stubSeeder{}
	searcher := NewFallbackSearcher(primary, provider, enabled, WithSeeder(seeder))
	searcher.spawnSeedWork = func(work func()) bool {
		work()

		return true
	}

	resp, err := searcher.Search(context.Background(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil {
		t.Fatalf("provider error must be swallowed, got %v", err)
	}
	if len(resp.Results) != 0 {
		t.Errorf("results = %#v, want empty", resp.Results)
	}
	if len(resp.PartialFailures) != 1 || resp.PartialFailures[0] != webProviderFailure() {
		t.Errorf("partial failures = %#v", resp.PartialFailures)
	}
	if seeder.calls != 0 {
		t.Fatalf("seeder = %#v", seeder)
	}
}

func TestFallbackKeepsAndSeedsVerifiedPartialProviderRows(t *testing.T) {
	seeder := &stubSeeder{}
	searcher := NewFallbackSearcher(
		&stubSearcher{},
		&stubProvider{
			results: []Result{{Title: "Web gap", URL: "https://web.example/gap"}},
			err:     errors.New("boom"),
		},
		enabled,
		WithSeeder(seeder),
	)
	searcher.spawnSeedWork = func(work func()) bool {
		work()

		return true
	}
	response, err := searcher.Search(
		t.Context(),
		searchcore.Request{Query: "gap", Limit: 10},
	)
	if err != nil || len(response.Results) != 1 || response.TotalResults != 1 ||
		response.Results[0].Source != searchcore.SourceWeb ||
		len(response.PartialFailures) != 1 || seeder.calls != 1 ||
		len(seeder.urls) != 1 || seeder.urls[0] != "https://web.example/gap" {
		t.Fatalf("response = %#v, seeder = %#v, error = %v", response, seeder, err)
	}
}

func TestFallbackPropagatesPrimaryError(t *testing.T) {
	sentinel := errors.New("primary down")
	primary := &stubSearcher{err: sentinel}
	provider := &stubProvider{}
	searcher := NewFallbackSearcher(primary, provider, enabled)

	if _, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: "x", Limit: 10},
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if provider.calls != 0 {
		t.Error("provider must not run when the primary errored")
	}
}

type stubSeeder struct {
	urls  []string
	calls int
	done  chan struct{}
}

func (s *stubSeeder) Seed(_ context.Context, urls []string) {
	s.calls++
	s.urls = urls
	if s.done != nil {
		close(s.done)
	}
}

func TestFallbackSeedsProviderURLs(t *testing.T) {
	primary := &stubSearcher{}
	provider := &stubProvider{results: []Result{
		{URL: "https://a.example/gap-intro"},
		{URL: "https://b.example/mind-the-gap"},
	}}
	seeder := &stubSeeder{done: make(chan struct{})}
	searcher := NewFallbackSearcher(primary, provider, enabled, WithSeeder(seeder))

	if _, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: "gap", Limit: 10},
	); err != nil {
		t.Fatalf("search: %v", err)
	}
	select {
	case <-seeder.done:
	case <-time.After(time.Second):
		t.Fatal("seeding did not run")
	}
	if seeder.calls != 1 || len(seeder.urls) != 2 {
		t.Fatalf("seeder = %#v", seeder)
	}
}

func TestFallbackDoesNotSeedWhenPrimaryAnswers(t *testing.T) {
	primary := &stubSearcher{
		resp: searchcore.Response{Results: []searchcore.Result{{Title: "owned"}}},
	}
	seeder := &stubSeeder{}
	searcher := NewFallbackSearcher(primary, &stubProvider{}, enabled, WithSeeder(seeder))

	if _, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: "x", Limit: 10},
	); err != nil {
		t.Fatalf("search: %v", err)
	}
	if seeder.calls != 0 {
		t.Error("must not seed when the primary already answered")
	}
}

func TestToCoreResultsCapsToLimit(t *testing.T) {
	results := toCoreResults([]Result{
		{URL: "https://a/"}, {URL: "https://b/"}, {URL: "https://c/"},
	}, 2)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
}

// TestFallbackSkipsNonTextVerticals pins SEARCH-40: an empty image (or any
// non-text vertical) result set must stay empty rather than silently turning
// into unfiltered text links from the web provider.
func TestFallbackSkipsNonTextVerticals(t *testing.T) {
	t.Parallel()

	for _, dom := range []searchcore.ContentDomain{
		searchcore.ContentDomainImage,
		searchcore.ContentDomainAudio,
		searchcore.ContentDomainVideo,
	} {
		provider := &stubProvider{results: []Result{{URL: "https://web.example", Title: "t"}}}
		searcher := NewFallbackSearcher(&stubSearcher{}, provider, enabled)
		resp, err := searcher.Search(context.Background(), searchcore.Request{
			Query:         "cats",
			ContentDomain: dom,
		})
		if err != nil {
			t.Fatalf("%s: %v", dom, err)
		}
		if provider.calls != 0 || len(resp.Results) != 0 {
			t.Fatalf("%s vertical must not fall back: calls=%d results=%d",
				dom, provider.calls, len(resp.Results))
		}
	}
}
