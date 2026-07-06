package snippetfetch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

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

// TestEnrichmentBuildsVerifiedSnippetsFromFetchedPages is the SEARCH-31
// acceptance on the reported bug: a peer row titled only «ДДТ (группа)» gains
// a body excerpt containing «осень», a fetched page missing the query words is
// sorted out, and an unreachable page keeps its row unchanged.
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
			peerRow("https://spam.example/page", "ддт осень скачать бесплатно"),
			peerRow("https://down.example/page", "ДДТ Что такое осень текст"),
		},
	}}, NewEnricher(fetcher.fetch))

	resp, err := search.Search(context.Background(), searchcore.Request{
		Query: "что такое осень ддт",
		Terms: []string{"что", "такое", "осень", "ддт"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 2 || resp.TotalResults != 2 {
		t.Fatalf("results = %d total = %d, want 2/2 (spam row sorted out)",
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
}

func TestEnrichmentSkipsWhenVerifyFalseAndCachesFetches(t *testing.T) {
	fetcher := &scriptedFetcher{pages: map[string]string{
		"https://ru.wikipedia.org/ddt": ddtArticle,
	}}
	enricher := NewEnricher(fetcher.fetch)
	search := WithSnippetEnrichment(innerSearcher{resp: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{
			peerRow("https://ru.wikipedia.org/ddt", "ДДТ (группа)"),
		},
	}}, enricher)

	trusting := searchcore.Request{
		Terms:  []string{"осень", "ддт"},
		Verify: searchcore.VerifyFalse,
	}
	resp, err := search.Search(context.Background(), trusting)
	if err != nil || fetcher.calls != 0 {
		t.Fatalf("verify=false fetched %d pages, err %v", fetcher.calls, err)
	}
	if resp.Results[0].Snippet != "ДДТ (группа)" {
		t.Fatalf("verify=false rewrote the snippet: %q", resp.Results[0].Snippet)
	}

	verifying := searchcore.Request{Terms: []string{"осень", "ддт"}}
	if _, err := search.Search(context.Background(), verifying); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, err := search.Search(context.Background(), verifying); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("cache miss count = %d, want 1", fetcher.calls)
	}
}

func TestEnrichmentTouchesOnlyLeadingPeerRows(t *testing.T) {
	fetcher := &scriptedFetcher{pages: map[string]string{}}
	rows := make([]searchcore.Result, 0, enrichLimit+2)
	rows = append(rows, searchcore.Result{
		Title: "локальный", URL: "https://local.example/", Source: searchcore.SourceLocal,
	})
	for i := range enrichLimit + 1 {
		rows = append(rows, peerRow(fmt.Sprintf("https://peer.example/p%d", i), "peer row"))
	}
	enricher := NewEnricher(fetcher.fetch)
	if _, dropped := enricher.enrich(
		context.Background(),
		[]string{"осень"},
		rows,
	); dropped != 0 {
		t.Fatalf("empty fetches dropped rows: %d", dropped)
	}
	if fetcher.calls != enrichLimit-1 {
		t.Fatalf("fetch calls = %d, want %d (leading peer rows only)",
			fetcher.calls, enrichLimit-1)
	}
}

func TestEnrichmentHelpersDegradeGracefully(t *testing.T) {
	if NewEnricher(nil) != nil {
		t.Fatal("nil fetcher must disable enrichment")
	}
	inner := innerSearcher{resp: searchcore.Response{}}
	if _, ok := WithSnippetEnrichment(inner, nil).(innerSearcher); !ok {
		t.Fatal("nil enricher must return the inner searcher")
	}
	enricher := NewEnricher(func(context.Context, string) (string, error) { return "", nil })
	if results, dropped := enricher.enrich(context.Background(), nil, nil); len(
		results,
	) != 0 || dropped != 0 {
		t.Fatal("empty inputs must pass through")
	}
	terms := enrichmentTerms(searchcore.Request{Query: "что такое"})
	if len(terms) != 2 {
		t.Fatalf("all-stopword fallback terms = %#v", terms)
	}
}

func TestQueryBiasedExcerptWindowsMidPageMatches(t *testing.T) {
	text := strings.Repeat("вводный текст без искомых слов вообще никаких. ", 20) +
		"Здесь начинается фрагмент про осень и листопад в городе. " +
		strings.Repeat("завершающий текст страницы после нужного фрагмента. ", 20)
	got := queryBiasedExcerpt(text, []string{"осень"})
	if !strings.HasPrefix(got, "… ") {
		t.Fatalf("mid-page excerpt lost its ellipsis: %.60q", got)
	}
	if !strings.Contains(got, "осень") {
		t.Fatalf("excerpt lost the term: %q", got)
	}
	short := queryBiasedExcerpt("короткий текст про осень", []string{"осень"})
	if short != "короткий текст про осень" {
		t.Fatalf("short text rewritten: %q", short)
	}
	missing := queryBiasedExcerpt(
		strings.Repeat("текст без нужных слов в начале страницы вообще. ", 20),
		[]string{"осень", " "},
	)
	if !strings.HasPrefix(missing, "текст без") {
		t.Fatalf("leading fallback lost: %.50q", missing)
	}
}

func TestSearchPropagatesInnerError(t *testing.T) {
	enricher := NewEnricher(func(context.Context, string) (string, error) { return "", nil })
	search := WithSnippetEnrichment(failingSearcher{}, enricher)
	if _, err := search.Search(context.Background(), searchcore.Request{}); err == nil {
		t.Fatal("inner error swallowed")
	}
}

type failingSearcher struct{}

func (failingSearcher) Search(context.Context, searchcore.Request) (searchcore.Response, error) {
	return searchcore.Response{}, errors.New("inner failed")
}

func TestPageTextCacheResetsWhenFull(t *testing.T) {
	calls := 0
	enricher := NewEnricher(func(_ context.Context, rawURL string) (string, error) {
		calls++
		return "текст " + rawURL, nil
	})
	for i := range cacheSize + 1 {
		if _, err := enricher.pageText(
			context.Background(),
			fmt.Sprintf("https://p.example/%d", i),
		); err != nil {
			t.Fatalf("pageText: %v", err)
		}
	}
	if calls != cacheSize+1 {
		t.Fatalf("fetch calls = %d", calls)
	}
	if len(enricher.cache) > cacheSize {
		t.Fatalf("cache grew past bound: %d", len(enricher.cache))
	}
}
