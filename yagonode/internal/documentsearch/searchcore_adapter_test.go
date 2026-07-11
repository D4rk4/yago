package documentsearch

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestCoreLocalSearcherReturnsLocalResults(t *testing.T) {
	word := yagomodel.WordHash("golang")
	urlHash := hashFor("doc1")
	searcher := NewLocalSearcher(
		fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
			word: {postingEntry(word, "doc1", 0, 3)},
		}},
		fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
			urlHash: metadataRow(t, urlHash, "https://example.org/docs/page.html", "Go YaCy"),
		}},
		100,
	)

	resp, err := searcher.Search(t.Context(), searchcore.Request{
		Query:         "golang",
		Terms:         []string{"golang"},
		Source:        searchcore.SourceLocal,
		Limit:         10,
		ContentDomain: searchcore.ContentDomainText,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.TotalResults != 1 || len(resp.Results) != 1 {
		t.Fatalf("response = %#v", resp)
	}
	result := resp.Results[0]
	if result.Title != "Go YaCy" ||
		result.URL != "https://example.org/docs/page.html" ||
		result.DisplayURL != "example.org/docs/page.html" ||
		result.File != "page.html" ||
		result.URLHash != urlHash.String() ||
		result.Score != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestCoreLocalSearcherUsesDocumentStoreSnippet(t *testing.T) {
	word := yagomodel.WordHash("golang")
	urlHash := hashFor("doc1")
	rawURL := "https://example.org/docs/page.html"
	searcher := NewLocalSearcherWithDocuments(
		fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
			word: {postingEntry(word, "doc1", 0, 3)},
		}},
		fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
			urlHash: metadataRow(t, urlHash, rawURL, "Metadata title"),
		}},
		fakeDocumentDirectory{documents: map[string]documentstore.Document{
			rawURL: {
				Title:         "Stored document title",
				ExtractedText: "First line\n\nsecond\tline with  spaces.",
			},
		}},
		100,
	)

	resp, err := searcher.Search(t.Context(), searchcore.Request{
		Terms:         []string{"golang"},
		Source:        searchcore.SourceLocal,
		Limit:         10,
		ContentDomain: searchcore.ContentDomainText,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %#v", resp.Results)
	}
	result := resp.Results[0]
	if result.Title != "Stored document title" ||
		result.Snippet != "First line second line with spaces." ||
		result.URL != rawURL ||
		result.URLHash != urlHash.String() {
		t.Fatalf("result = %#v", result)
	}
}

func TestCoreLocalSearcherFallsBackWhenDocumentMissing(t *testing.T) {
	word := yagomodel.WordHash("golang")
	urlHash := hashFor("doc1")
	searcher := NewLocalSearcherWithDocuments(
		fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
			word: {postingEntry(word, "doc1", 0, 3)},
		}},
		fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
			urlHash: metadataRow(
				t,
				urlHash,
				"https://example.org/docs/page.html",
				"Metadata title",
			),
		}},
		fakeDocumentDirectory{},
		100,
	)

	resp, err := searcher.Search(t.Context(), searchcore.Request{
		Terms: []string{"golang"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Snippet != "Metadata title" {
		t.Fatalf("results = %#v", resp.Results)
	}
}

func TestCoreLocalSearcherUsesDocumentTitleWhenTextEmpty(t *testing.T) {
	word := yagomodel.WordHash("golang")
	urlHash := hashFor("doc1")
	rawURL := "https://example.org/docs/page.html"
	searcher := NewLocalSearcherWithDocuments(
		fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
			word: {postingEntry(word, "doc1", 0, 3)},
		}},
		fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
			urlHash: metadataRow(t, urlHash, rawURL, "Metadata title"),
		}},
		fakeDocumentDirectory{documents: map[string]documentstore.Document{
			rawURL: {Title: "Stored title"},
		}},
		100,
	)

	resp, err := searcher.Search(t.Context(), searchcore.Request{
		Terms: []string{"golang"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 ||
		resp.Results[0].Title != "Stored title" ||
		resp.Results[0].Snippet != "Stored title" {
		t.Fatalf("results = %#v", resp.Results)
	}
}

func TestCoreLocalSearcherRejectsDocumentLookupError(t *testing.T) {
	word := yagomodel.WordHash("golang")
	urlHash := hashFor("doc1")
	sentinel := errors.New("document down")
	searcher := NewLocalSearcherWithDocuments(
		fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
			word: {postingEntry(word, "doc1", 0, 3)},
		}},
		fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
			urlHash: metadataRow(
				t,
				urlHash,
				"https://example.org/docs/page.html",
				"Metadata title",
			),
		}},
		fakeDocumentDirectory{err: sentinel},
		100,
	)

	_, err := searcher.Search(t.Context(), searchcore.Request{
		Terms: []string{"golang"},
		Limit: 10,
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Search error = %v, want %v", err, sentinel)
	}
}

func TestCoreSearchSnippetBoundsText(t *testing.T) {
	text := strings.Repeat("å", searchCoreSnippetRuneCap+1)
	got := searchCoreSnippet("prefix\n" + text)
	if len([]rune(got)) != searchCoreSnippetRuneCap {
		t.Fatalf("snippet runes = %d, want %d", len([]rune(got)), searchCoreSnippetRuneCap)
	}
}

func TestCoreLocalSearcherFiltersAndOffsets(t *testing.T) {
	word := yagomodel.WordHash("golang")
	first := hashFor("doc1")
	second := hashFor("doc2")
	third := hashFor("doc3")
	searcher := NewLocalSearcher(
		fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
			word: {
				postingEntry(word, "doc1", 0, 3),
				postingEntry(word, "doc2", 0, 2),
				postingEntry(word, "doc3", 0, 1),
			},
		}},
		fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
			first:  metadataRow(t, first, "https://example.org/a.pdf", "First"),
			second: metadataRow(t, second, "https://docs.example.org/b.pdf", "Second"),
			third:  metadataRow(t, third, "https://example.net/c.txt", "Third"),
		}},
		100,
	)

	resp, err := searcher.Search(t.Context(), searchcore.Request{
		Terms:            []string{"golang"},
		Source:           searchcore.SourceGlobal,
		Limit:            1,
		Offset:           1,
		ContentDomain:    searchcore.ContentDomainText,
		InURL:            "example",
		TLD:              "org",
		FileType:         "pdf",
		URLMaskFilter:    `https://.*`,
		PreferMaskFilter: `docs`,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].URL != "https://example.org/a.pdf" {
		t.Fatalf("results = %#v", resp.Results)
	}
	if len(resp.PartialFailures) != 0 {
		t.Fatalf("partial failures = %#v", resp.PartialFailures)
	}
}

func TestCoreLocalSearcherHandlesEmptyOffsetWindow(t *testing.T) {
	resp, err := NewLocalSearcher(
		fakeScanner{},
		fakeDirectory{},
		100,
	).Search(t.Context(), searchcore.Request{Offset: 10, Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("results = %#v", resp.Results)
	}
}

func TestCoreLocalSearcherReturnsErrors(t *testing.T) {
	sentinel := errors.New("boom")
	cases := []struct {
		name     string
		searcher searchcore.Searcher
		req      searchcore.Request
	}{
		{
			name: "bad site",
			searcher: NewLocalSearcher(
				fakeScanner{},
				fakeDirectory{},
				100,
			),
			req: searchcore.Request{SiteHost: "."},
		},
		{
			name: "scan",
			searcher: NewLocalSearcher(
				fakeScanner{err: sentinel},
				fakeDirectory{},
				100,
			),
			req: searchcore.Request{Terms: []string{"w1"}},
		},
		{
			name: "bad url metadata url",
			searcher: NewLocalSearcher(
				fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
					yagomodel.WordHash("w1"): {
						postingEntry(yagomodel.WordHash("w1"), "u1", 0, 1),
					},
				}},
				fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{
					hashFor("u1"): {
						Properties: map[string]string{
							yagomodel.URLMetaHash: hashFor("u1").String(),
							yagomodel.URLMetaURL:  "z|@@@",
						},
					},
				}},
				100,
			),
			req: searchcore.Request{Terms: []string{"w1"}},
		},
		{
			name: "bad url mask",
			searcher: NewLocalSearcher(
				fakeScanner{},
				fakeDirectory{},
				100,
			),
			req: searchcore.Request{URLMaskFilter: "["},
		},
		{
			name: "bad prefer mask",
			searcher: NewLocalSearcher(
				fakeScanner{},
				fakeDirectory{},
				100,
			),
			req: searchcore.Request{PreferMaskFilter: "["},
		},
	}
	for _, tc := range cases {
		if _, err := tc.searcher.Search(context.Background(), tc.req); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestCoreResultFallbacks(t *testing.T) {
	hash := hashFor("doc1")
	result, err := searchCoreResult(
		searchCoreResultContext{
			ctx: t.Context(),
			req: searchcore.Request{Source: searchcore.SourceLocal},
		},
		yagomodel.URIMetadataRow{Properties: map[string]string{
			yagomodel.URLMetaHash: hash.String(),
			yagomodel.URLMetaURL:  yagomodel.EncodeBase64WireForm("not a url"),
			"size":                "12",
			yagomodel.ColModDate:  "20260101",
		}},
		0,
		1,
	)
	if err != nil {
		t.Fatalf("searchCoreResult: %v", err)
	}
	if result.Title != "not a url" ||
		result.Host != "" ||
		result.DisplayURL != "not%20a%20url" ||
		result.File != "not a url" ||
		result.Size != 12 {
		t.Fatalf("result = %#v", result)
	}
}

func TestCoreResultRejectsBadHash(t *testing.T) {
	_, err := searchCoreResult(
		searchCoreResultContext{ctx: t.Context()},
		yagomodel.URIMetadataRow{Properties: map[string]string{
			yagomodel.URLMetaHash: "bad",
		}},
		0,
		1,
	)
	if err == nil {
		t.Fatal("expected hash error")
	}
}

func TestCoreResultRejectsBadTitle(t *testing.T) {
	_, err := searchCoreResult(
		searchCoreResultContext{ctx: t.Context()},
		yagomodel.URIMetadataRow{Properties: map[string]string{
			yagomodel.URLMetaHash:           hashFor("doc1").String(),
			yagomodel.URLMetaURL:            yagomodel.EncodeBase64WireForm("https://example.org/"),
			yagomodel.URLMetaColDescription: "z|@@@",
		}},
		0,
		1,
	)
	if err == nil {
		t.Fatal("expected title error")
	}
}

func TestCoreCriteriaCoversContentKindsAndSiteHash(t *testing.T) {
	kinds := map[searchcore.ContentDomain]contentKind{
		searchcore.ContentDomainImage: imageContent,
		searchcore.ContentDomainAudio: audioContent,
		searchcore.ContentDomainVideo: videoContent,
		searchcore.ContentDomainApp:   applicationContent,
		searchcore.ContentDomainAll:   anyContent,
	}
	for domain, want := range kinds {
		got, err := searchCoreCriteria(searchcore.Request{
			ContentDomain: domain,
			SiteHost:      "example.org",
		})
		if err != nil {
			t.Fatalf("searchCoreCriteria(%s): %v", domain, err)
		}
		if got.contentKind != want || got.siteHash == "" {
			t.Fatalf("criteria(%s) = %#v, want kind %d and site hash", domain, got, want)
		}
	}
}

func TestParsedURLPartsHandlesNilAndDirectories(t *testing.T) {
	host, pathValue, file := parsedURLParts(nil)
	if host != "" || pathValue != "" || file != "" {
		t.Fatalf("nil parts = %q %q %q", host, pathValue, file)
	}

	_, _, file = parsedURLParts(mustParseURL(t, "https://example.org/"))
	if file != "" {
		t.Fatalf("file = %q, want empty", file)
	}
}

func TestCoreResultMatchersRejectEachFilter(t *testing.T) {
	cases := []searchcore.Request{
		{URLMaskFilter: "allowed"},
		{InURL: "allowed"},
		{TLD: "net"},
		{FileType: "pdf"},
	}
	for _, req := range cases {
		matchers, err := newCoreResultMatchers(req)
		if err != nil {
			t.Fatalf("newCoreResultMatchers: %v", err)
		}
		if matchers.match(searchcore.Result{
			URL:  "https://example.org/file.html",
			Host: "example.org",
			File: "file.html",
		}) {
			t.Fatalf("matchers(%#v) accepted result", req)
		}
	}
}

func TestCoreResultMatchersApplyLocalSafeSearch(t *testing.T) {
	text := coreResultMatchers{req: searchcore.Request{
		SafeSearch: true, ContentDomain: searchcore.ContentDomainText,
	}}
	if text.match(searchcore.Result{SafetyRating: searchcore.SafetyExplicit}) {
		t.Fatal("explicit local result was accepted")
	}
	if !text.match(searchcore.Result{}) {
		t.Fatal("unknown local text result was rejected")
	}
	image := coreResultMatchers{req: searchcore.Request{
		SafeSearch: true, ContentDomain: searchcore.ContentDomainImage,
	}}
	if image.match(searchcore.Result{}) {
		t.Fatal("unknown local image result was accepted")
	}
	if !image.match(searchcore.Result{SafetyRating: searchcore.SafetyGeneral}) {
		t.Fatal("general local image result was rejected")
	}
}

func mustParseURL(tb testing.TB, raw string) *url.URL {
	tb.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		tb.Fatalf("url.Parse: %v", err)
	}

	return parsed
}

func metadataRow(
	tb testing.TB,
	hash yagomodel.Hash,
	rawURL string,
	title string,
) yagomodel.URIMetadataRow {
	tb.Helper()

	return yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaHash:           hash.String(),
		yagomodel.URLMetaURL:            yagomodel.EncodeBase64WireForm(rawURL),
		yagomodel.URLMetaColDescription: yagomodel.EncodeBase64WireForm(title),
	}}
}

type fakeDocumentDirectory struct {
	documents map[string]documentstore.Document
	err       error
}

func (d fakeDocumentDirectory) Document(
	_ context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	if d.err != nil {
		return documentstore.Document{}, false, d.err
	}
	doc, found := d.documents[normalizedURL]

	return doc, found, nil
}

func (d fakeDocumentDirectory) Count(context.Context) (int, error) {
	return len(d.documents), d.err
}
