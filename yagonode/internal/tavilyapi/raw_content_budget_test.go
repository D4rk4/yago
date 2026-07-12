package tavilyapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func retainedStringAddress(value string) uintptr {
	return uintptr(reflect.ValueOf(value).UnsafePointer())
}

func TestRawContentBudgetExactAndPlusOne(t *testing.T) {
	retained := &rawContentBudget{}
	if !retained.reserve(maximumRawContentResponseBytes, 0) || retained.reserve(1, 0) {
		t.Fatalf("retained budget = %+v", retained)
	}
	output := &rawContentBudget{}
	if !output.reserve(0, maximumRawContentResponseBytes) || output.reserve(0, 1) {
		t.Fatalf("output budget = %+v", output)
	}
}

func TestRequestIDLengthAndRetentionBoundary(t *testing.T) {
	backing := strings.Repeat("x", 1<<20)
	exact := backing[100 : 100+maximumRequestIDBytes]
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, PathSearch, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set(requestIDHeader, exact)
	retained := requestID(request)
	if retained != exact || retainedStringAddress(retained) == retainedStringAddress(exact) {
		t.Fatal("exact request ID was not independently retained")
	}
	request.Header.Set(requestIDHeader, exact+"x")
	if generated := requestID(request); generated == exact+"x" ||
		len(generated) > maximumRequestIDBytes {
		t.Fatalf("plus-one request ID = %q", generated)
	}
}

func TestRawContentJSONStringBytesMatchesEncoder(t *testing.T) {
	values := []string{
		"plain",
		"quote\" slash\\",
		"\b\f\n\r\t",
		"\x00<>&",
		"Привет\u2028мир\u2029",
		string([]byte{0xff}),
	}
	for _, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %q: %v", value, err)
		}
		if got := rawContentJSONStringBytes(value); got != len(encoded) {
			t.Fatalf("encoded size %q = %d, want %d", value, got, len(encoded))
		}
	}
	oversized := strings.Repeat("<", maximumRawContentResponseBytes/6+1)
	if got := rawContentJSONStringBytes(oversized); got != maximumRawContentResponseBytes+1 {
		t.Fatalf("oversized encoded bytes = %d", got)
	}
}

func TestBoundedMarkdownRendering(t *testing.T) {
	doc := documentstore.Document{
		Title:         "Title",
		Headings:      []string{" Section ", ""},
		ExtractedText: "\nSection\n- item\n* other\nparagraph\n",
	}
	got, ok := boundedDocumentMarkdown(doc, maximumRawContentResponseBytes)
	if !ok || got != documentMarkdown(doc) {
		t.Fatalf("bounded markdown = %q %t", got, ok)
	}
	if _, ok := boundedDocumentMarkdown(doc, 1); ok {
		t.Fatal("title must exceed the bound")
	}
	tooMany := make([]string, maximumMarkdownHeadingBytes/rawContentMapEntryBytes+1)
	if _, ok := boundedMarkdownHeadings(tooMany); ok {
		t.Fatal("heading collection must exceed the bound")
	}
	if _, ok := boundedDocumentMarkdown(documentstore.Document{Headings: tooMany}, 1); ok {
		t.Fatal("document heading collection must exceed the bound")
	}
	if _, ok := boundedMarkdownHeadings([]string{
		strings.Repeat("h", maximumMarkdownHeadingBytes),
	}); ok {
		t.Fatal("heading payload must exceed the bound")
	}
	for _, candidate := range []documentstore.Document{
		{Headings: []string{"heading"}, ExtractedText: "heading"},
		{ExtractedText: "- item"},
		{ExtractedText: "paragraph"},
	} {
		if _, ok := boundedDocumentMarkdown(candidate, 1); ok {
			t.Fatalf("content must exceed the bound: %+v", candidate)
		}
	}
	if got, ok := boundedFetchedMarkdown(FetchedContent{Text: "x"}, 1); !ok || got != "x" {
		t.Fatalf("text markdown = %q %t", got, ok)
	}
	if _, ok := boundedFetchedMarkdown(FetchedContent{Text: "xx"}, 1); ok {
		t.Fatal("text markdown must exceed the bound")
	}
	if got, ok := boundedFetchedMarkdown(
		FetchedContent{Title: "t", Text: "x"},
		6,
	); !ok || got != "# t\n\nx" {
		t.Fatalf("fetched markdown = %q %t", got, ok)
	}
	if _, ok := boundedFetchedMarkdown(FetchedContent{Title: "title"}, 4); ok {
		t.Fatal("title markdown must exceed the bound")
	}
	if _, ok := boundedFetchedMarkdown(FetchedContent{Title: "t", Text: "xx"}, 6); ok {
		t.Fatal("combined markdown must exceed the bound")
	}
}

func TestRawContentBudgetExhaustionBranches(t *testing.T) {
	full := &rawContentBudget{retained: maximumRawContentResponseBytes}
	if _, err := retainExtractFailure(full, "url", "failure"); !errors.Is(
		err,
		errRawContentBudgetExceeded,
	) {
		t.Fatalf("extract failure error = %v", err)
	}
	if _, ok := retainCrawlURL(full, "https://example.com/"); ok {
		t.Fatal("crawl URL retained after budget exhaustion")
	}
	walker := &crawlWalker{
		bounds:  crawlBoundsSet{breadth: 1, limit: 2},
		filters: crawlFilters{baseHost: "example.com"},
		budget:  full,
		seen:    map[string]bool{"https://example.com/": true},
	}
	walker.enqueueLinks([]string{"https://example.com/next"}, 1)
	if len(walker.seen) != 1 {
		t.Fatalf("seen after exhausted enqueue = %d", len(walker.seen))
	}
	walker.request.Format = "markdown"
	if _, err := walker.retainResult(
		"https://example.com/",
		CrawledPage{Title: "title", Text: "text"},
	); !errors.Is(err, errRawContentBudgetExceeded) {
		t.Fatalf("crawl markdown error = %v", err)
	}
	tooMany := make([]string, maximumMarkdownHeadingBytes/rawContentMapEntryBytes+1)
	if _, failure, err := retainDocumentExtractResult(
		ExtractRequest{Format: "markdown"},
		"https://example.com/",
		documentstore.Document{Headings: tooMany},
		&rawContentBudget{},
	); err != nil || failure == nil {
		t.Fatalf("document rejection failure=%v error=%v", failure, err)
	}
	if _, failure, err := retainFetchedExtractResult(
		ExtractRequest{Format: "markdown"},
		"https://example.com/",
		FetchedContent{Text: strings.Repeat("x", maximumRawContentResponseBytes+1)},
		&rawContentBudget{},
	); err != nil || failure == nil {
		t.Fatalf("fetch rejection failure=%v error=%v", failure, err)
	}
}

func TestRawSearchRejectsUnboundedMarkdownMetadata(t *testing.T) {
	url := "https://markdown.example/"
	tooMany := make([]string, maximumMarkdownHeadingBytes/rawContentMapEntryBytes+1)
	endpoint := searchEndpoint{documents: &fakeDocuments{rows: map[string]documentstore.Document{
		url: {ExtractedText: "text", Headings: tooMany},
	}}}
	_, _, _, err := endpoint.responseResult(
		t.Context(),
		SearchRequest{IncludeRawContent: rawContentMode("markdown")},
		searchcore.Request{},
		searchcore.Result{URL: url},
	)
	if !errors.Is(err, errRawContentBudgetExceeded) {
		t.Fatalf("markdown metadata error = %v", err)
	}
}

func maximumExtractTextBytes(id, url string) int {
	return maximumExtractTextBytesForCount(id, url, 1)
}

func maximumExtractTextBytesForCount(id, url string, count int) int {
	budget := &rawContentBudget{}
	budget.reserve(
		rawContentEnvelopeBytes+len(id)+
			count*(rawContentExtractResultBytes+rawContentExtractFailureBytes),
		rawContentEnvelopeBytes+rawContentJSONStringBytes(id),
	)
	retained := maximumRawContentResponseBytes - budget.retained - len(url)
	output := maximumRawContentResponseBytes - budget.output -
		rawContentResultJSONBytes - rawContentJSONStringBytes(url) -
		rawContentJSONStringBytes("") - 2

	return min(retained, output)
}

func TestExtractResponseBudgetExactAndPlusOne(t *testing.T) {
	const (
		id  = "id"
		url = "https://exact.example/"
	)
	maximum := maximumExtractTextBytes(id, url)
	endpoint := extractEndpoint{
		documents: &fakeDocuments{rows: map[string]documentstore.Document{
			url: {ExtractedText: strings.Repeat("x", maximum)},
		}},
		now: time.Now,
	}
	response, err := endpoint.extractResponse(
		context.Background(), ExtractRequest{URLs: urlList{url}}, time.Now(), id,
	)
	if err != nil || len(response.Results) != 1 ||
		len(response.Results[0].RawContent) != maximum {
		t.Fatalf("exact response results=%d error=%v", len(response.Results), err)
	}
	endpoint.documents = &fakeDocuments{rows: map[string]documentstore.Document{
		url: {ExtractedText: strings.Repeat("x", maximum+1)},
	}}
	response, err = endpoint.extractResponse(
		context.Background(), ExtractRequest{URLs: urlList{url}}, time.Now(), id,
	)
	if err != nil || len(response.Results) != 0 || len(response.FailedResults) != 1 ||
		!strings.Contains(response.FailedResults[0].Error, "response limit") {
		t.Fatalf("plus-one response=%+v error=%v", response, err)
	}
}

func TestExtractResponseDetachesRetainedFields(t *testing.T) {
	backing := strings.Repeat("x", 1<<20)
	url := backing[10:40]
	raw := backing[50:80]
	image := backing[90:120]
	result, failure, err := retainExtractResult(
		&rawContentBudget{}, url, raw, []string{image}, false,
	)
	if err != nil || failure != nil {
		t.Fatalf("retain result failure=%v error=%v", failure, err)
	}
	if retainedStringAddress(result.URL) == retainedStringAddress(url) ||
		retainedStringAddress(result.RawContent) == retainedStringAddress(raw) ||
		retainedStringAddress(result.Images[0]) == retainedStringAddress(image) {
		t.Fatal("extract result retained source backing storage")
	}
}

func TestRawSearchAndCrawlDetachRetainedFields(t *testing.T) {
	backing := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 1<<16)
	fields := []string{
		backing[10:20],
		backing[30:50],
		backing[60:80],
		backing[90:110],
		backing[120:140],
		backing[150:170],
		backing[180:200],
		backing[210:230],
		backing[240:260],
	}
	raw := fields[3]
	item := SearchResult{
		Title: fields[0], URL: fields[1], Content: fields[2], RawContent: &raw,
		PublishedDate: fields[4], Favicon: fields[5], Source: fields[6],
		Images: []string{fields[7]},
	}
	retained, images, err := retainRawSearchResult(
		newRawSearchResultBudget(1),
		item,
		[]SearchImage{{URL: fields[7], Description: fields[8]}},
	)
	if err != nil {
		t.Fatalf("retain search result: %v", err)
	}
	retainedFields := []string{
		retained.Title,
		retained.URL,
		retained.Content,
		*retained.RawContent,
		retained.PublishedDate,
		retained.Favicon,
		retained.Source,
		retained.Images[0],
		images[0].URL,
		images[0].Description,
	}
	sourceFields := []string{
		fields[0], fields[1], fields[2], fields[3], fields[4],
		fields[5], fields[6], fields[7], fields[7], fields[8],
	}
	for index := range retainedFields {
		if retainedStringAddress(retainedFields[index]) ==
			retainedStringAddress(sourceFields[index]) {
			t.Fatalf("search field %d retained source backing storage", index)
		}
	}
	crawlURL, ok := retainCrawlURL(&rawContentBudget{}, fields[1])
	if !ok || retainedStringAddress(crawlURL) == retainedStringAddress(fields[1]) {
		t.Fatal("crawl URL retained source backing storage")
	}
}

func maximumSearchRawTextBytes(item SearchResult) int {
	budget := newRawSearchResultBudget(1)
	retained := maximumRawContentResponseBytes - budget.retained -
		len(item.Title) - len(item.URL) - len(item.Content) - len(item.PublishedDate) -
		len(item.Favicon) - len(item.Source)
	output := maximumRawContentResponseBytes - budget.output -
		rawContentResultJSONBytes - rawContentJSONStringBytes(item.Title) -
		rawContentJSONStringBytes(item.URL) - rawContentJSONStringBytes(item.Content) -
		rawContentJSONStringBytes(item.PublishedDate) -
		rawContentJSONStringBytes(item.Favicon) - rawContentJSONStringBytes(item.Source) - 2

	return min(retained, output)
}

func TestRawSearchResultBudgetExactAndPlusOne(t *testing.T) {
	item := SearchResult{Title: "t", URL: "https://exact.example/", Content: "c"}
	maximum := maximumSearchRawTextBytes(item)
	exact := strings.Repeat("x", maximum)
	item.RawContent = &exact
	budget := newRawSearchResultBudget(1)
	retained, images, err := retainRawSearchResult(budget, item, nil)
	if err != nil || len(*retained.RawContent) != maximum || len(images) != 0 {
		t.Fatalf(
			"exact raw result=%d images=%d error=%v",
			len(*retained.RawContent),
			len(images),
			err,
		)
	}
	plusOne := strings.Repeat("x", maximum+1)
	item.RawContent = &plusOne
	budget = newRawSearchResultBudget(1)
	if _, _, err := retainRawSearchResult(budget, item, nil); !errors.Is(
		err,
		errRawContentBudgetExceeded,
	) {
		t.Fatalf("plus-one error = %v", err)
	}
}

type singlePageFetcher struct {
	page CrawledPage
}

func (f singlePageFetcher) FetchPage(context.Context, string) (CrawledPage, error) {
	return f.page, nil
}

func maximumCrawlTextBytes(url string) int {
	budget := &rawContentBudget{}
	budget.reserve(
		rawContentEnvelopeBytes+rawContentCrawlResultBytes,
		rawContentEnvelopeBytes,
	)
	retainCrawlURL(budget, url)
	budget.reserve(0, rawContentJSONStringBytes(url))
	retained := maximumRawContentResponseBytes - budget.retained
	output := maximumRawContentResponseBytes - budget.output -
		rawContentResultJSONBytes - rawContentJSONStringBytes(url) -
		rawContentJSONStringBytes("") - 2

	return min(retained, output)
}

func TestCrawlResponseBudgetExactAndPlusOne(t *testing.T) {
	const url = "https://exact.example/"
	limit := 1
	maximum := maximumCrawlTextBytes(url)
	endpoint := crawlEndpoint{fetcher: singlePageFetcher{page: CrawledPage{
		Text: strings.Repeat("x", maximum),
	}}}
	entries, _, err := endpoint.walk(
		context.Background(), CrawlRequest{URL: url, Limit: &limit},
	)
	if err != nil || len(entries) != 1 || len(entries[0].RawContent) != maximum {
		t.Fatalf("exact entries=%d error=%v", len(entries), err)
	}
	endpoint.fetcher = singlePageFetcher{page: CrawledPage{
		Text: strings.Repeat("x", maximum+1),
	}}
	if _, _, err := endpoint.walk(
		context.Background(), CrawlRequest{URL: url, Limit: &limit},
	); !errors.Is(err, errRawContentBudgetExceeded) {
		t.Fatalf("plus-one error = %v", err)
	}
}
