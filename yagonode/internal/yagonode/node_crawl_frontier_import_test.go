package yagonode

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type frontierPresenceFixture struct {
	existing map[string]bool
	present  []bool
	err      error
}

func (f frontierPresenceFixture) DocumentExists(
	_ context.Context,
	url string,
) (bool, error) {
	return f.existing[url], nil
}

func (f frontierPresenceFixture) DocumentsExist(
	_ context.Context,
	urls []string,
) ([]bool, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.present != nil {
		return append([]bool(nil), f.present...), nil
	}
	found := make([]bool, len(urls))
	for index, rawURL := range urls {
		found[index] = f.existing[rawURL]
	}
	return found, nil
}

type frontierOrderQueueFixture struct {
	key   string
	order yagocrawlcontract.CrawlOrder
	seen  map[string]bool
	calls int
	err   error
}

func (f *frontierOrderQueueFixture) PublishOnce(
	_ context.Context,
	key string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	if f.seen[key] {
		return true, nil
	}
	f.seen[key] = true
	f.key = key
	f.order = order
	return false, nil
}

type frontierProfileWriterFixture struct {
	profiles int
	err      error
}

func (f *frontierProfileWriterFixture) RecordProfile(
	context.Context,
	yagocrawlcontract.CrawlProfile,
	yagocrawlcontract.CrawlOrderPriority,
) error {
	f.profiles++
	return f.err
}

func TestCrawlOrderImportSkipsIndexedURLsAndRetriesIdempotently(t *testing.T) {
	existingURL := "https://example.test/already-indexed"
	newURL := "https://example.test/pending"
	batch := validFrontierImportBatch(t, existingURL, newURL)
	queue := &frontierOrderQueueFixture{seen: make(map[string]bool)}
	profiles := new(frontierProfileWriterFixture)
	service := &crawlOrderImportService{
		documents: frontierPresenceFixture{existing: map[string]bool{existingURL: true}},
		orders:    queue,
		profiles:  profiles,
	}

	result, err := service.ImportCrawlOrder(t.Context(), batch)
	if err != nil {
		t.Fatalf("import frontier: %v", err)
	}
	if result.EnqueuedRequests != 1 || result.AlreadyIndexed != 1 || result.DuplicateBatch {
		t.Fatalf("first import result = %+v", result)
	}
	if len(queue.order.Requests) != 1 || queue.order.Requests[0].URL != newURL {
		t.Fatalf("persisted requests = %+v", queue.order.Requests)
	}
	if queue.key != batch.IdempotencyKey() || profiles.profiles != 1 {
		t.Fatalf("queue key %q, profile registrations %d", queue.key, profiles.profiles)
	}

	result, err = service.ImportCrawlOrder(t.Context(), batch)
	if err != nil {
		t.Fatalf("retry frontier: %v", err)
	}
	if !result.DuplicateBatch || result.EnqueuedRequests != 0 || result.AlreadyIndexed != 1 {
		t.Fatalf("retry result = %+v", result)
	}
	if queue.calls != 2 {
		t.Fatalf("queue calls = %d, want idempotent retry through queue", queue.calls)
	}
}

func TestCrawlOrderImportLeavesAllIndexedURLsOutOfQueue(t *testing.T) {
	firstURL := "https://example.test/one"
	secondURL := "https://example.test/two"
	batch := validFrontierImportBatch(t, firstURL, secondURL)
	queue := &frontierOrderQueueFixture{seen: make(map[string]bool)}
	service := &crawlOrderImportService{
		documents: frontierPresenceFixture{
			existing: map[string]bool{firstURL: true, secondURL: true},
		},
		orders:   queue,
		profiles: new(frontierProfileWriterFixture),
	}
	result, err := service.ImportCrawlOrder(t.Context(), batch)
	if err != nil {
		t.Fatalf("import indexed frontier: %v", err)
	}
	if result.AlreadyIndexed != 2 || result.EnqueuedRequests != 0 || queue.calls != 0 {
		t.Fatalf("result %+v, queue calls %d", result, queue.calls)
	}
}

func TestCrawlOrderImportDoesNotPublishWhenProfilePersistenceFails(t *testing.T) {
	batch := validFrontierImportBatch(t, "https://example.test/page")
	queue := &frontierOrderQueueFixture{seen: make(map[string]bool)}
	service := &crawlOrderImportService{
		documents: frontierPresenceFixture{existing: map[string]bool{}},
		orders:    queue,
		profiles:  &frontierProfileWriterFixture{err: errors.New("storage unavailable")},
	}
	if _, err := service.ImportCrawlOrder(t.Context(), batch); err == nil {
		t.Fatal("profile storage failure was ignored")
	}
	if queue.calls != 0 {
		t.Fatal("order was published before profile persistence succeeded")
	}
}

func TestCrawlOrderImportRejectsInvalidAndUnavailableInputs(t *testing.T) {
	valid := validFrontierImportBatch(t, "https://example.test/page")
	tests := []struct {
		name     string
		batch    yagocrawlcontract.CrawlOrderImportBatch
		docs     documentstore.DocumentSetPresence
		wantText string
	}{
		{
			name:     "invalid batch",
			batch:    invalidFrontierImportBatch(valid),
			docs:     frontierPresenceFixture{},
			wantText: "validate imported frontier",
		},
		{
			name:     "missing document lookup",
			batch:    valid,
			wantText: "document presence lookup unavailable",
		},
		{
			name:     "lookup failure",
			batch:    valid,
			docs:     frontierPresenceFixture{err: errors.New("presence unavailable")},
			wantText: "check imported frontier URLs",
		},
		{
			name:     "incomplete lookup",
			batch:    valid,
			docs:     frontierPresenceFixture{present: []bool{}},
			wantText: "incomplete result",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &crawlOrderImportService{
				documents: test.docs,
				orders:    &frontierOrderQueueFixture{seen: make(map[string]bool)},
				profiles:  new(frontierProfileWriterFixture),
			}
			if _, err := service.ImportCrawlOrder(t.Context(), test.batch); err == nil ||
				!strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("ImportCrawlOrder error = %v, want %q", err, test.wantText)
			}
		})
	}
}

func TestCrawlOrderImportReturnsQueuePersistenceFailure(t *testing.T) {
	batch := validFrontierImportBatch(t, "https://example.test/page")
	service := &crawlOrderImportService{
		documents: frontierPresenceFixture{existing: map[string]bool{}},
		orders: &frontierOrderQueueFixture{
			seen: make(map[string]bool),
			err:  errors.New("queue unavailable"),
		},
		profiles: new(frontierProfileWriterFixture),
	}
	if _, err := service.ImportCrawlOrder(t.Context(), batch); err == nil ||
		!strings.Contains(err.Error(), "persist imported crawl frontier") {
		t.Fatalf("queue persistence error = %v", err)
	}
}

func TestCrawlRuntimeDelegatesCrawlOrderImport(t *testing.T) {
	batch := validFrontierImportBatch(t, "https://runtime.example/page")
	queue := &frontierOrderQueueFixture{seen: make(map[string]bool)}
	service := &crawlOrderImportService{
		documents: frontierPresenceFixture{existing: map[string]bool{}},
		orders:    queue,
		profiles:  new(frontierProfileWriterFixture),
	}
	runtime := &crawlRuntime{frontierImport: service}
	result, err := runtime.ImportCrawlOrder(t.Context(), batch)
	if err != nil || result.EnqueuedRequests != 1 || queue.calls != 1 {
		t.Fatalf("runtime import result = %+v, queue calls=%d, err=%v", result, queue.calls, err)
	}
	if _, err := runtime.ImportCrawlOrder(
		t.Context(),
		invalidFrontierImportBatch(batch),
	); err == nil ||
		!strings.Contains(err.Error(), "import crawl order through crawl runtime") {
		t.Fatalf("runtime import error = %v", err)
	}
}

func invalidFrontierImportBatch(
	batch yagocrawlcontract.CrawlOrderImportBatch,
) yagocrawlcontract.CrawlOrderImportBatch {
	batch.Version++
	return batch
}

func validFrontierImportBatch(
	t *testing.T,
	urls ...string,
) yagocrawlcontract.CrawlOrderImportBatch {
	t.Helper()
	profile := yagocrawlcontract.CrawlProfile{
		Handle:          "fixture-profile",
		Name:            "sanitized fixture",
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	}
	requests := make([]yagocrawlcontract.CrawlRequest, 0, len(urls))
	for _, rawURL := range urls {
		requests = append(requests, yagocrawlcontract.CrawlRequest{
			URL:           rawURL,
			ProfileHandle: profile.Handle,
		})
	}
	batch := yagocrawlcontract.CrawlOrderImportBatch{
		Version:  1,
		SourceID: "sanitized-source",
		Profile:  profile,
		Requests: requests,
	}
	var err error
	batch.BatchID, err = batch.ComputeBatchID()
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

var _ adminui.CrawlOrderImporter = (*crawlOrderImportService)(nil)
