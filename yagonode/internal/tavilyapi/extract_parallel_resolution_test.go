package tavilyapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const extractTestCompletionTimeout = 5 * time.Second

type boundedExtractFetcher struct {
	mutex        sync.Mutex
	active       int
	maximum      int
	calls        int
	saturated    chan struct{}
	saturatedOne sync.Once
	release      chan struct{}
}

type repeatedExtractFetcher struct {
	mutex sync.Mutex
	calls int
}

func (f *repeatedExtractFetcher) Fetch(
	_ context.Context,
	url string,
) (FetchedContent, error) {
	f.mutex.Lock()
	f.calls++
	f.mutex.Unlock()

	return FetchedContent{Text: url}, nil
}

func (f *repeatedExtractFetcher) totalCalls() int {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	return f.calls
}

func (f *boundedExtractFetcher) Fetch(
	ctx context.Context,
	url string,
) (FetchedContent, error) {
	f.mutex.Lock()
	f.active++
	f.calls++
	if f.active > f.maximum {
		f.maximum = f.active
	}
	if f.active == maximumConcurrentExtractURLResolutions {
		f.saturatedOne.Do(func() { close(f.saturated) })
	}
	f.mutex.Unlock()
	select {
	case <-f.release:
	case <-ctx.Done():
	}
	f.mutex.Lock()
	f.active--
	f.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return FetchedContent{}, fmt.Errorf("bounded extract fetch: %w", err)
	}

	return FetchedContent{Text: url}, nil
}

func (f *boundedExtractFetcher) activity() (active, maximum, calls int) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	return f.active, f.maximum, f.calls
}

type sequencedExtractFetcher struct {
	started   chan string
	completed chan string
	releases  map[string]chan struct{}
	content   string
}

func (f sequencedExtractFetcher) Fetch(
	ctx context.Context,
	url string,
) (FetchedContent, error) {
	select {
	case f.started <- url:
	case <-ctx.Done():
		return FetchedContent{}, fmt.Errorf("start sequenced extract fetch: %w", ctx.Err())
	}
	select {
	case <-f.releases[url]:
	case <-ctx.Done():
		return FetchedContent{}, fmt.Errorf("release sequenced extract fetch: %w", ctx.Err())
	}
	select {
	case f.completed <- url:
	case <-ctx.Done():
		return FetchedContent{}, fmt.Errorf("complete sequenced extract fetch: %w", ctx.Err())
	}

	return FetchedContent{Text: f.content}, nil
}

type cancelingExtractFetcher struct {
	mutex      sync.Mutex
	active     int
	started    int
	finished   int
	allStarted chan struct{}
	startedOne sync.Once
}

func (f *cancelingExtractFetcher) Fetch(
	ctx context.Context,
	_ string,
) (FetchedContent, error) {
	f.mutex.Lock()
	f.active++
	f.started++
	if f.started == maximumConcurrentExtractURLResolutions-1 {
		f.startedOne.Do(func() { close(f.allStarted) })
	}
	f.mutex.Unlock()
	<-ctx.Done()
	f.mutex.Lock()
	f.active--
	f.finished++
	f.mutex.Unlock()

	return FetchedContent{}, fmt.Errorf("fetch canceled: %w", ctx.Err())
}

func (f *cancelingExtractFetcher) activity() (active, started, finished int) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	return f.active, f.started, f.finished
}

type fatalExtractDocuments struct {
	fetchesStarted <-chan struct{}
	fatalURL       string
}

func (d fatalExtractDocuments) Document(
	ctx context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	if normalizedURL != d.fatalURL {
		return documentstore.Document{}, false, nil
	}
	select {
	case <-d.fetchesStarted:
		return documentstore.Document{}, false, errors.New("store unavailable")
	case <-ctx.Done():
		return documentstore.Document{}, false, fmt.Errorf(
			"wait for extract fetches: %w",
			ctx.Err(),
		)
	}
}

func (fatalExtractDocuments) Count(context.Context) (int, error) {
	return 0, nil
}

func TestExtractParallelResolutionBoundsFetchConcurrency(t *testing.T) {
	urls := make([]string, maximumConcurrentExtractURLResolutions*2)
	for index := range urls {
		urls[index] = fmt.Sprintf("https://bounded.example/%02d", index)
	}
	fetcher := &boundedExtractFetcher{
		saturated: make(chan struct{}),
		release:   make(chan struct{}),
	}
	handler := NewExtractEndpointWithFetcher(
		nil,
		SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher,
	)
	finished := make(chan *extractHTTPResult, 1)
	go func() {
		finished <- executeExtractHTTP(handler, urls)
	}()
	waitForExtractTestSignal(t, fetcher.saturated, "extract workers did not saturate")
	active, maximum, calls := fetcher.activity()
	if active != maximumConcurrentExtractURLResolutions ||
		maximum != maximumConcurrentExtractURLResolutions ||
		calls != maximumConcurrentExtractURLResolutions {
		t.Fatalf("active=%d maximum=%d calls=%d", active, maximum, calls)
	}
	close(fetcher.release)
	result := waitForExtractHTTPResult(t, finished)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.status != http.StatusOK {
		t.Fatalf("status=%d body=%s", result.status, result.body)
	}
	if active, maximum, calls = fetcher.activity(); active != 0 ||
		maximum != maximumConcurrentExtractURLResolutions || calls != len(urls) {
		t.Fatalf("final active=%d maximum=%d calls=%d", active, maximum, calls)
	}
	if len(result.response.Results) != len(urls) {
		t.Fatalf("results=%d", len(result.response.Results))
	}
	for index, url := range urls {
		if result.response.Results[index].URL != url {
			t.Fatalf("result[%d]=%q, want %q", index, result.response.Results[index].URL, url)
		}
	}
}

func TestExtractParallelResolutionRetainsRepeatedInputs(t *testing.T) {
	const requestedURL = "https://repeated.example/page"
	fetcher := &repeatedExtractFetcher{}
	handler := NewExtractEndpointWithFetcher(
		nil,
		SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher,
	)
	result := runExtractHTTP(t, handler, []string{requestedURL, requestedURL})
	if result.status != http.StatusOK {
		t.Fatalf("status=%d body=%s", result.status, result.body)
	}
	if len(result.response.Results) != 2 || len(result.response.FailedResults) != 0 {
		t.Fatalf(
			"results=%d failures=%d",
			len(result.response.Results),
			len(result.response.FailedResults),
		)
	}
	for position, extracted := range result.response.Results {
		if extracted.URL != requestedURL {
			t.Fatalf("result[%d]=%q", position, extracted.URL)
		}
	}
	if calls := fetcher.totalCalls(); calls != 2 {
		t.Fatalf("fetch calls=%d", calls)
	}
}

func TestExtractOrderedResolutionWindowKeepsBudgetAndInputOrder(t *testing.T) {
	urls := make([]string, maximumConcurrentExtractURLResolutions+1)
	releases := make(map[string]chan struct{}, len(urls))
	for index := range urls {
		urls[index] = fmt.Sprintf("https://ordered.example/%02d", index)
		releases[urls[index]] = make(chan struct{})
	}
	fetcher := sequencedExtractFetcher{
		started:   make(chan string, len(urls)),
		completed: make(chan string),
		releases:  releases,
		content:   strings.Repeat("x", 4<<20),
	}
	endpoint := extractEndpoint{fetcher: fetcher, now: time.Now}
	finished := make(chan extractResponseResult, 1)
	go func() {
		response, err := endpoint.extractResponse(
			t.Context(),
			ExtractRequest{URLs: urlList(urls)},
			time.Now(),
			"ordered",
		)
		finished <- extractResponseResult{response: response, err: err}
	}()
	started := make(map[string]bool, maximumConcurrentExtractURLResolutions)
	for range maximumConcurrentExtractURLResolutions {
		url := waitForExtractURL(t, fetcher.started, "extract fetch did not start")
		started[url] = true
	}
	for _, url := range urls[:maximumConcurrentExtractURLResolutions] {
		if !started[url] {
			t.Fatalf("initial fetches = %#v", started)
		}
	}
	for _, index := range []int{3, 2, 1} {
		completeExtractURL(t, fetcher, urls[index])
	}
	assertNoExtractURL(t, fetcher.started, "extract window advanced before its first outcome")
	completeExtractURL(t, fetcher, urls[0])
	if startedURL := waitForExtractURL(
		t,
		fetcher.started,
		"last extract fetch did not start",
	); startedURL != urls[4] {
		t.Fatalf("last started URL = %q", startedURL)
	}
	completeExtractURL(t, fetcher, urls[4])
	result := waitForExtractResponseResult(t, finished)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if len(result.response.Results) != 3 || len(result.response.FailedResults) != 2 {
		t.Fatalf(
			"results=%d failures=%d",
			len(result.response.Results),
			len(result.response.FailedResults),
		)
	}
	for index, result := range result.response.Results {
		if result.URL != urls[index] {
			t.Fatalf("result[%d]=%q, want %q", index, result.URL, urls[index])
		}
	}
	for index, failure := range result.response.FailedResults {
		if failure.URL != urls[index+3] || !strings.Contains(failure.Error, "response limit") {
			t.Fatalf("failure[%d]=%#v", index, failure)
		}
	}
}

type panickingExtractFetcher struct {
	mutex      sync.Mutex
	panicURL   string
	active     int
	started    int
	finished   int
	allStarted chan struct{}
	startedOne sync.Once
}

func (f *panickingExtractFetcher) Fetch(
	ctx context.Context,
	url string,
) (FetchedContent, error) {
	f.mutex.Lock()
	f.active++
	f.started++
	if f.started == maximumConcurrentExtractURLResolutions {
		f.startedOne.Do(func() { close(f.allStarted) })
	}
	f.mutex.Unlock()
	defer func() {
		f.mutex.Lock()
		f.active--
		f.finished++
		f.mutex.Unlock()
	}()
	if url == f.panicURL {
		select {
		case <-f.allStarted:
			panic("dependency implementation detail")
		case <-ctx.Done():
			return FetchedContent{}, fmt.Errorf("wait for panicking extract fetches: %w", ctx.Err())
		}
	}
	<-ctx.Done()

	return FetchedContent{}, fmt.Errorf("wait for extract cancellation: %w", ctx.Err())
}

func (f *panickingExtractFetcher) activity() (active, started, finished int) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	return f.active, f.started, f.finished
}

func TestExtractWorkerPanicReturnsStableErrorAndJoinsWorkers(t *testing.T) {
	const panicURL = "https://panic.example/"
	fetcher := &panickingExtractFetcher{
		panicURL:   panicURL,
		allStarted: make(chan struct{}),
	}
	handler := NewExtractEndpointWithFetcher(
		nil,
		SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher,
	)
	result := runExtractHTTP(t, handler, []string{
		panicURL,
		"https://blocked-one.example/",
		"https://blocked-two.example/",
		"https://blocked-three.example/",
		"https://pending.example/",
	})
	if result.status != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", result.status, result.body)
	}
	var response ErrorResponse
	if err := json.Unmarshal([]byte(result.body), &response); err != nil {
		t.Fatal(err)
	}
	if response.Detail.Error != errExtractURLResolutionPanicked.Error() ||
		strings.Contains(result.body, "implementation detail") {
		t.Fatalf("error response=%s", result.body)
	}
	active, started, finished := fetcher.activity()
	if active != 0 || started != maximumConcurrentExtractURLResolutions || finished != started {
		t.Fatalf("active=%d started=%d finished=%d", active, started, finished)
	}
}

func TestExtractFatalStoreErrorCancelsAndWaitsForWorkers(t *testing.T) {
	const fatalURL = "https://fatal.example/"
	fetcher := &cancelingExtractFetcher{allStarted: make(chan struct{})}
	urls := []string{
		"https://fetch-one.example/",
		"https://fetch-two.example/",
		"https://fetch-three.example/",
		fatalURL,
		"https://pending.example/",
	}
	handler := NewExtractEndpointWithFetcher(
		fatalExtractDocuments{fetchesStarted: fetcher.allStarted, fatalURL: fatalURL},
		SearchAccessPolicy{BearerToken: extractTestKey},
		fetcher,
	)
	result := runExtractHTTP(t, handler, urls)
	if result.status != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", result.status, result.body)
	}
	active, started, finished := fetcher.activity()
	if active != 0 || started != maximumConcurrentExtractURLResolutions-1 ||
		finished != started {
		t.Fatalf("active=%d started=%d finished=%d", active, started, finished)
	}
}

type extractHTTPResult struct {
	status   int
	body     string
	response ExtractResponse
	err      error
}

type extractResponseResult struct {
	response ExtractResponse
	err      error
}

func runExtractHTTP(
	t *testing.T,
	handler http.Handler,
	urls []string,
) *extractHTTPResult {
	t.Helper()
	result := executeExtractHTTP(handler, urls)
	if result.err != nil {
		t.Fatal(result.err)
	}

	return result
}

func executeExtractHTTP(handler http.Handler, urls []string) *extractHTTPResult {
	encoded, err := json.Marshal(urls)
	if err != nil {
		return &extractHTTPResult{err: err}
	}
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		PathExtract,
		strings.NewReader(`{"urls":`+string(encoded)+`}`),
	)
	request.Header.Set("Authorization", "Bearer "+extractTestKey)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	result := &extractHTTPResult{status: recorder.Code, body: recorder.Body.String()}
	if recorder.Code == http.StatusOK {
		result.err = json.Unmarshal(recorder.Body.Bytes(), &result.response)
	}

	return result
}

func waitForExtractTestSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(extractTestCompletionTimeout):
		t.Fatal(failure)
	}
}

func waitForExtractHTTPResult(
	t *testing.T,
	results <-chan *extractHTTPResult,
) *extractHTTPResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(extractTestCompletionTimeout):
		t.Fatal("extract HTTP request did not finish")

		return nil
	}
}

func waitForExtractURL(t *testing.T, urls <-chan string, failure string) string {
	t.Helper()
	select {
	case url := <-urls:
		return url
	case <-time.After(extractTestCompletionTimeout):
		t.Fatal(failure)

		return ""
	}
}

func assertNoExtractURL(t *testing.T, urls <-chan string, failure string) {
	t.Helper()
	select {
	case url := <-urls:
		t.Fatalf("%s: %s", failure, url)
	case <-time.After(50 * time.Millisecond):
	}
}

func completeExtractURL(t *testing.T, fetcher sequencedExtractFetcher, url string) {
	t.Helper()
	close(fetcher.releases[url])
	if completed := waitForExtractURL(
		t,
		fetcher.completed,
		"extract fetch did not finish",
	); completed != url {
		t.Fatalf("completed URL = %q, want %q", completed, url)
	}
}

func waitForExtractResponseResult(
	t *testing.T,
	results <-chan extractResponseResult,
) extractResponseResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(extractTestCompletionTimeout):
		t.Fatal("extract response did not finish")

		return extractResponseResult{}
	}
}
