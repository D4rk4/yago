package snippetfetch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

var ddtArticle = "ДДТ — советская и российская рок-группа, основанная Юрием " +
	"Шевчуком. Самая известная песня группы — «Что такое осень», записанная " +
	"для альбома Актриса Весна; композиция стала визитной карточкой коллектива " +
	"и звучит на каждом концерте, а клип на неё снимали вместе с музыкантами " +
	"групп Алиса и Кино. " + strings.Repeat(
	"Дискография коллектива насчитывает десятки альбомов, среди которых "+
		"пластинки разных десятилетий и концертные записи. ", 6)

type scriptedFetcher struct {
	mu    sync.Mutex
	pages map[string]string
	errs  map[string]error
	calls int
}

func (f *scriptedFetcher) fetch(_ context.Context, rawURL string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if err, ok := f.errs[rawURL]; ok {
		return "", err
	}

	return f.pages[rawURL], nil
}

type innerSearcher struct{ resp searchcore.Response }

func (s innerSearcher) Search(context.Context, searchcore.Request) (searchcore.Response, error) {
	return s.resp, nil
}

func peerRow(url, title string) searchcore.Result {
	return searchcore.Result{
		Title:   title,
		URL:     url,
		Snippet: title,
		Source:  searchcore.SourceRemote,
	}
}

func testTextEvidence(
	_ context.Context,
	text string,
	terms []string,
	_ string,
) (TextEvidence, bool) {
	folded := strings.ToLower(text)
	start := len(text)
	end := 0
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		at := strings.Index(folded, term)
		if at < 0 {
			return TextEvidence{}, false
		}
		start = min(start, at)
		end = max(end, at+len(term))
	}
	if end <= start {
		return TextEvidence{}, false
	}

	return TextEvidence{Start: start, End: end}, true
}

func TestSafeSearchBelowEnrichmentPreventsUnknownRemoteFetch(t *testing.T) {
	fetcher := &scriptedFetcher{pages: map[string]string{
		"https://unknown.example/": "query body",
	}}
	search := WithSnippetEnrichment(
		searchcore.NewSafeSearchSearcher(innerSearcher{resp: searchcore.Response{
			Results: []searchcore.Result{peerRow("https://unknown.example/", "query")},
		}}),
		NewEnricher(fetcher.fetch),
		testTextEvidence,
	)
	response, err := search.Search(t.Context(), searchcore.Request{
		Query: "query", Terms: []string{"query"}, SafeSearch: true,
	})
	if err != nil || len(response.Results) != 0 || fetcher.calls != 0 {
		t.Fatalf("response/fetches = %#v/%d/%v", response, fetcher.calls, err)
	}
}

func TestEnrichmentBuildsVerifiedSnippetsFromFetchedPages(t *testing.T) {
	fetcher := &scriptedFetcher{
		pages: map[string]string{
			"https://ru.wikipedia.org/ddt": ddtArticle,
			"https://spam.example/page":    "страница про совершенно другие вещи и темы без нужных слов вовсе",
		},
		errs: map[string]error{"https://down.example/page": errors.New("unreachable")},
	}
	search := WithSnippetEnrichment(innerSearcher{resp: searchcore.Response{
		TotalResults: 3,
		Results: []searchcore.Result{
			peerRow("https://ru.wikipedia.org/ddt", "ДДТ (группа) — Википедия"),
			peerRow("https://spam.example/page", "скачать бесплатно"),
			peerRow("https://down.example/page", "ДДТ Что такое осень текст"),
		},
	}}, NewEnricher(fetcher.fetch), testTextEvidence)

	resp, err := search.Search(context.Background(), searchcore.Request{
		Query: "что такое осень ддт",
		Terms: []string{"что", "такое", "осень", "ддт"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 2 || resp.TotalResults != 2 {
		t.Fatalf("results = %d total = %d, want two evidence-bearing rows",
			len(resp.Results), resp.TotalResults)
	}
	wiki := resp.Results[0]
	if !strings.Contains(strings.ToLower(wiki.Snippet), "осень") {
		t.Fatalf("wiki snippet lost the content term: %q", wiki.Snippet)
	}
	down := resp.Results[1]
	if down.Snippet != "ДДТ Что такое осень текст" {
		t.Fatalf("unreachable row rewritten: %q", down.Snippet)
	}
	if fetcher.calls != 2 {
		t.Fatalf("fetch calls = %d, want two unmatched visible rows", fetcher.calls)
	}
}

func TestEnrichmentDropsEvidenceFreeWindow(t *testing.T) {
	fetcher := &scriptedFetcher{pages: map[string]string{
		"https://uk.wikipedia.org/ddt": "ДДТ — радянський і російський рок-гурт, пісня «Що таке осінь» відома всім",
	}}
	search := WithSnippetEnrichment(innerSearcher{resp: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{
			peerRow("https://uk.wikipedia.org/ddt", "ДДТ (гурт) — Вікіпедія"),
		},
	}}, NewEnricher(fetcher.fetch), testTextEvidence)
	response, err := search.Search(context.Background(), searchcore.Request{
		Terms: []string{"осень", "ддт"},
	})
	if err != nil || len(response.Results) != 0 || response.TotalResults != 0 {
		t.Fatalf("response = %#v, err = %v", response, err)
	}
}

func TestEnrichmentVerifyFalseFiltersWithoutFetchingAndFetchesAreCached(t *testing.T) {
	fetcher := &scriptedFetcher{pages: map[string]string{
		"https://ru.wikipedia.org/ddt": ddtArticle,
	}}
	enricher := NewEnricher(fetcher.fetch)
	search := WithSnippetEnrichment(innerSearcher{resp: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{
			peerRow("https://ru.wikipedia.org/ddt", "ДДТ (группа)"),
		},
	}}, enricher, testTextEvidence)

	trusting := searchcore.Request{
		Terms:  []string{"осень", "ддт"},
		Verify: searchcore.VerifyFalse,
	}
	resp, err := search.Search(context.Background(), trusting)
	if err != nil || fetcher.calls != 0 {
		t.Fatalf("verify=false fetched %d pages, err %v", fetcher.calls, err)
	}
	if len(resp.Results) != 0 || resp.TotalResults != 0 {
		t.Fatalf("verify=false admitted evidence-free row: %#v", resp)
	}

	verifying := searchcore.Request{Terms: []string{"осень", "ддт"}}
	first, err := search.Search(context.Background(), verifying)
	if err != nil || len(first.Results) != 1 {
		t.Fatalf("first verifying search = %#v, err = %v", first, err)
	}
	if _, err := search.Search(context.Background(), verifying); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("cache miss count = %d, want 1", fetcher.calls)
	}
}

func TestEnrichmentVerifyFalseKeepsVisibleEvidence(t *testing.T) {
	search := WithSnippetEnrichment(innerSearcher{resp: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{
			peerRow("https://peer.example/other", "Visible query evidence"),
		},
	}}, nil, testTextEvidence)
	response, err := search.Search(t.Context(), searchcore.Request{
		Terms: []string{"query"}, Verify: searchcore.VerifyFalse,
	})
	if err != nil || len(response.Results) != 1 || response.TotalResults != 1 {
		t.Fatalf("response = %#v, err = %v", response, err)
	}
}

func TestEnrichmentCacheOnlyNeverFetches(t *testing.T) {
	fetcher := &scriptedFetcher{pages: map[string]string{
		"https://a.example/": "query text from the page",
	}}
	enricher := NewEnricher(fetcher.fetch)
	search := WithSnippetEnrichment(innerSearcher{resp: searchcore.Response{
		Results: []searchcore.Result{peerRow("https://a.example/", "peer title")},
	}}, enricher, testTextEvidence)
	request := searchcore.Request{
		Query: "query", Terms: []string{"query"}, Verify: searchcore.VerifyCacheOnly,
	}

	response, err := search.Search(t.Context(), request)
	if err != nil || fetcher.calls != 0 || len(response.Results) != 0 {
		t.Fatalf("cold cache-only response = %#v calls=%d err=%v", response, fetcher.calls, err)
	}
	request.Verify = searchcore.VerifyIfExist
	if _, err := search.Search(t.Context(), request); err != nil {
		t.Fatalf("warming search: %v", err)
	}
	request.Verify = searchcore.VerifyCacheOnly
	response, err = search.Search(t.Context(), request)
	if err != nil || fetcher.calls != 1 ||
		!strings.Contains(response.Results[0].Snippet, "query") {
		t.Fatalf("warm cache-only response = %#v calls=%d err=%v", response, fetcher.calls, err)
	}
}

func TestEnrichmentFetchesOnlyThreeUnmatchedPeerRowsAndDropsTheRest(t *testing.T) {
	fetcher := &scriptedFetcher{pages: map[string]string{}}
	rows := make([]searchcore.Result, 0, enrichLimit+2)
	rows = append(rows, searchcore.Result{
		Title: "локальный", URL: "https://local.example/", Source: searchcore.SourceLocal,
	})
	for i := range enrichLimit + 1 {
		rows = append(rows, peerRow(fmt.Sprintf("https://peer.example/p%d", i), "peer row"))
	}
	search := WithSnippetEnrichment(innerSearcher{resp: searchcore.Response{
		TotalResults: len(rows), Results: rows,
	}}, NewEnricher(fetcher.fetch), testTextEvidence)
	response, err := search.Search(context.Background(), searchcore.Request{
		Terms: []string{"осень"},
	})
	if err != nil || len(response.Results) != 1 || response.TotalResults != 1 ||
		response.Results[0].Source != searchcore.SourceLocal {
		t.Fatalf("response = %#v, err = %v", response, err)
	}
	if fetcher.calls != enrichLimit {
		t.Fatalf("fetch calls = %d, want %d (leading peer rows only)",
			fetcher.calls, enrichLimit)
	}
}

func TestEnrichmentHelpersDegradeGracefully(t *testing.T) {
	if NewEnricher(nil) != nil {
		t.Fatal("nil fetcher must disable enrichment")
	}
	inner := innerSearcher{resp: searchcore.Response{}}
	_ = WithSnippetEnrichment(inner, nil, testTextEvidence)
	terms := enrichmentTerms(searchcore.Request{Query: "что такое"})
	if len(terms) != 2 {
		t.Fatalf("all-stopword fallback terms = %#v", terms)
	}
	search := WithSnippetEnrichment(innerSearcher{resp: searchcore.Response{
		TotalResults: 2,
		Results: []searchcore.Result{
			{Title: "local", Source: searchcore.SourceLocal},
			peerRow("https://peer.example/", "peer"),
		},
	}}, nil, testTextEvidence)
	response, err := search.Search(t.Context(), searchcore.Request{Terms: []string{"query"}})
	if err != nil || len(response.Results) != 1 || response.TotalResults != 1 {
		t.Fatalf("nil enricher response = %#v, err = %v", response, err)
	}
}

func TestEvidenceExcerptRejectsInvalidAndOversizedEvidence(t *testing.T) {
	invalidUTF8 := string([]byte{0xff})
	for _, test := range []struct {
		text     string
		evidence TextEvidence
	}{
		{text: "text", evidence: TextEvidence{Start: -1, End: 1}},
		{text: invalidUTF8, evidence: TextEvidence{Start: 0, End: 1}},
		{text: strings.Repeat("x", excerptRuneCap+1), evidence: TextEvidence{Start: 0, End: excerptRuneCap + 1}},
		{text: " ", evidence: TextEvidence{Start: 0, End: 1}},
	} {
		if excerpt, ok := evidenceExcerpt(test.text, test.evidence); ok || excerpt != "" {
			t.Fatalf("excerpt = %q, ok = %v", excerpt, ok)
		}
	}
}

func TestEvidenceExcerptCentersCompleteSpan(t *testing.T) {
	prefix := strings.Repeat("prefix   ", 100)
	text := prefix + "query evidence" + strings.Repeat(" suffix", 100)
	excerpt, ok := evidenceExcerpt(text, TextEvidence{
		Start: len(prefix), End: len(prefix) + len("query evidence"),
	})
	if !ok || !strings.HasPrefix(excerpt, "… ") ||
		!strings.Contains(excerpt, "query evidence") ||
		len([]rune(excerpt)) > excerptRuneCap {
		t.Fatalf("excerpt = %q, ok = %v", excerpt, ok)
	}
}

func TestPageOutcomeRejectsInvalidOrLostEvidence(t *testing.T) {
	invalid := enrichingSearcher{match: func(
		context.Context,
		string,
		[]string,
		string,
	) (TextEvidence, bool) {
		return TextEvidence{Start: 0, End: 100}, true
	}}
	if outcome := invalid.pageOutcome(t.Context(), "query", []string{"query"}, ""); outcome.keep {
		t.Fatalf("invalid evidence outcome = %#v", outcome)
	}
	calls := 0
	lost := enrichingSearcher{match: func(
		_ context.Context,
		text string,
		_ []string,
		_ string,
	) (TextEvidence, bool) {
		calls++

		return TextEvidence{Start: 0, End: len(text)}, calls == 1
	}}
	if outcome := lost.pageOutcome(t.Context(), "query", []string{"query"}, ""); outcome.keep {
		t.Fatalf("lost evidence outcome = %#v", outcome)
	}
}

func TestVisibleResultTextRetainsMalformedEscapes(t *testing.T) {
	text := visibleResultText(searchcore.Result{URL: "https://peer.example/%zz"})
	if !strings.Contains(text, "%zz") {
		t.Fatalf("visible text = %q", text)
	}
}

func TestSearchPropagatesInnerError(t *testing.T) {
	enricher := NewEnricher(func(context.Context, string) (string, error) { return "", nil })
	search := WithSnippetEnrichment(failingSearcher{}, enricher, testTextEvidence)
	if _, err := search.Search(context.Background(), searchcore.Request{}); err == nil {
		t.Fatal("inner error swallowed")
	}
}

type failingSearcher struct{}

func (failingSearcher) Search(context.Context, searchcore.Request) (searchcore.Response, error) {
	return searchcore.Response{}, errors.New("inner failed")
}

func TestPageTextCacheBoundsEntries(t *testing.T) {
	calls := 0
	enricher := NewEnricher(func(_ context.Context, rawURL string) (string, error) {
		calls++
		return "текст " + rawURL, nil
	})
	for i := range cacheMaximumEntries + 1 {
		if _, err := enricher.pageText(
			context.Background(),
			fmt.Sprintf("https://p.example/%d", i),
		); err != nil {
			t.Fatalf("pageText: %v", err)
		}
	}
	if calls != cacheMaximumEntries+1 {
		t.Fatalf("fetch calls = %d", calls)
	}
	if len(enricher.cache.entries) > cacheMaximumEntries {
		t.Fatalf("cache grew past bound: %d", len(enricher.cache.entries))
	}
}

func TestPageTextReturnsFetchError(t *testing.T) {
	want := errors.New("fetch failed")
	enricher := NewEnricher(func(context.Context, string) (string, error) {
		return "", want
	})
	if _, err := enricher.pageText(t.Context(), "https://peer.example/"); !errors.Is(err, want) {
		t.Fatalf("pageText error = %v", err)
	}
}

func TestPageTextFetchAdmissionIsSharedAndCancellationAware(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	enricher := NewEnricher(func(context.Context, string) (string, error) {
		close(started)
		<-release

		return "query text", nil
	})
	enricher.slots = make(chan struct{}, 1)
	firstDone := make(chan error, 1)
	go func() {
		_, err := enricher.pageText(t.Context(), "https://a.example/")
		firstDone <- err
	}()
	<-started
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := enricher.pageText(
		canceled,
		"https://b.example/",
	); !errors.Is(
		err,
		context.Canceled,
	) {
		close(release)
		<-firstDone
		t.Fatalf("admission error = %v", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first fetch: %v", err)
	}
}

func TestPageTextCoalescesWaitersThroughTheCache(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	var mu sync.Mutex
	enricher := NewEnricher(func(context.Context, string) (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		close(started)
		<-release

		return "query text", nil
	})
	enricher.slots = make(chan struct{}, 1)
	results := make(chan string, 2)
	load := func() {
		text, err := enricher.pageText(t.Context(), "https://a.example/")
		if err != nil {
			results <- err.Error()

			return
		}
		results <- text
	}
	go load()
	<-started
	go load()
	select {
	case result := <-results:
		close(release)
		<-results
		t.Fatalf("waiter completed before fetch release: %q", result)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	for range 2 {
		if result := <-results; result != "query text" {
			t.Fatalf("page text = %q", result)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("fetch calls = %d, want 1", calls)
	}
}
