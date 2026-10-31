package yagonode

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
)

type importDocumentDirectory map[string]documentstore.Document

func (d importDocumentDirectory) Document(
	_ context.Context,
	url string,
) (documentstore.Document, bool, error) {
	document, found := d[url]
	return document, found, nil
}

func (importDocumentDirectory) Count(context.Context) (int, error) { return 0, nil }

func TestImportWorkerForURLHandlesWorkerCountBounds(t *testing.T) {
	for _, workerCount := range []int{
		-1, 0, 1, yagocrawlcontract.MaximumIngestBatchRecords + 1,
	} {
		if got := importWorkerForURL("https://example.test/", workerCount); got != 0 {
			t.Fatalf("worker index with count %d = %d, want 0", workerCount, got)
		}
	}
	for index := range 128 {
		url := fmt.Sprintf("https://example.test/%d", index)
		got := importWorkerForURL(url, 4)
		if got < 0 || got >= 4 {
			t.Fatalf("worker index for %q = %d, outside [0,4)", url, got)
		}
		if repeated := importWorkerForURL(url, 4); repeated != got {
			t.Fatalf("worker assignment for %q changed from %d to %d", url, got, repeated)
		}
	}
}

func TestDocumentImportWorkerCountCapsConcurrentWorkers(t *testing.T) {
	if got := documentImportWorkerCount(importConcurrencyFixture(128), 128); got != 64 {
		t.Fatalf("worker count = %d, want the 64-worker bound", got)
	}
	for _, maximum := range []int{-1, 0} {
		if got := documentImportWorkerCount(importConcurrencyFixture(maximum), 4); got != 1 {
			t.Errorf(
				"worker count with maximum %d = %d, want the one-worker fallback",
				maximum,
				got,
			)
		}
	}
}

type failingImportDocumentDirectory struct{ err error }

func (d failingImportDocumentDirectory) Document(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	return documentstore.Document{}, false, d.err
}

func (failingImportDocumentDirectory) Count(context.Context) (int, error) { return 0, nil }

type batchImportDocumentDirectory struct {
	mu        sync.Mutex
	documents map[string]documentstore.Document
	batchURLs []string
	batchErr  error
	lookups   int
}

func (d *batchImportDocumentDirectory) Document(
	_ context.Context,
	url string,
) (documentstore.Document, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lookups++
	document, found := d.documents[url]
	return document, found, nil
}

func (d *batchImportDocumentDirectory) Documents(
	_ context.Context,
	urls []string,
) ([]documentstore.Document, []bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.batchErr != nil {
		return nil, nil, d.batchErr
	}
	d.batchURLs = append(d.batchURLs, urls...)
	documents := make([]documentstore.Document, len(urls))
	found := make([]bool, len(urls))
	for index, url := range urls {
		documents[index], found[index] = d.documents[url]
	}
	return documents, found, nil
}

func (d *batchImportDocumentDirectory) Count(context.Context) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.documents), nil
}

func (d *batchImportDocumentDirectory) store(document documentstore.Document) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.documents[document.NormalizedURL] = document
}

type batchImportSubmitterFixture struct {
	mu        sync.Mutex
	directory *batchImportDocumentDirectory
	batches   []yagocrawlcontract.IngestBatch
}

func (f *batchImportSubmitterFixture) MaximumConcurrentImports() int { return 4 }

func (f *batchImportSubmitterFixture) SubmitImport(
	_ context.Context,
	batch yagocrawlcontract.IngestBatch,
	_ yagocrawlcontract.CrawlProfile,
) error {
	f.mu.Lock()
	f.batches = append(f.batches, batch)
	f.mu.Unlock()
	f.directory.store(documentstore.Document{
		NormalizedURL: batch.Document.NormalizedURL,
		ContentHash:   batch.Document.ContentHash,
	})
	return nil
}

type importSubmitterFixture struct {
	batches []yagocrawlcontract.IngestBatch
	docs    importDocumentDirectory
	err     error
}

func (f *importSubmitterFixture) SubmitImport(
	_ context.Context,
	batch yagocrawlcontract.IngestBatch,
	_ yagocrawlcontract.CrawlProfile,
) error {
	f.batches = append(f.batches, batch)
	if f.err != nil {
		return f.err
	}
	if f.docs != nil {
		f.docs[batch.Document.NormalizedURL] = documentstore.Document{
			NormalizedURL: batch.Document.NormalizedURL,
			ContentHash:   batch.Document.ContentHash,
		}
	}
	return nil
}

func (*importSubmitterFixture) MaximumConcurrentImports() int { return 1 }

type cancelingImportSubmitter struct {
	cancel context.CancelFunc
}

func (s cancelingImportSubmitter) SubmitImport(
	context.Context,
	yagocrawlcontract.IngestBatch,
	yagocrawlcontract.CrawlProfile,
) error {
	s.cancel()
	return nil
}

type importConcurrencyFixture int

func (importConcurrencyFixture) SubmitImport(
	context.Context,
	yagocrawlcontract.IngestBatch,
	yagocrawlcontract.CrawlProfile,
) error {
	return nil
}

func (f importConcurrencyFixture) MaximumConcurrentImports() int { return int(f) }

type incompleteBatchImportDocumentDirectory struct {
	*batchImportDocumentDirectory
}

func (*incompleteBatchImportDocumentDirectory) Documents(
	context.Context,
	[]string,
) ([]documentstore.Document, []bool, error) {
	return nil, nil, nil
}

type concurrentImportDirectory struct {
	mu        sync.RWMutex
	documents map[string]documentstore.Document
}

func (d *concurrentImportDirectory) Document(
	_ context.Context,
	url string,
) (documentstore.Document, bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	document, found := d.documents[url]
	return document, found, nil
}

func (d *concurrentImportDirectory) Count(context.Context) (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.documents), nil
}

func (d *concurrentImportDirectory) store(document documentstore.Document) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.documents[document.NormalizedURL] = document
}

type concurrentImportSubmitterFixture struct {
	mu        sync.Mutex
	directory *concurrentImportDirectory
	active    int
	maximum   int
	limit     int
	seen      []string
}

func (f *concurrentImportSubmitterFixture) MaximumConcurrentImports() int {
	return f.limit
}

func (f *concurrentImportSubmitterFixture) SubmitImport(
	ctx context.Context,
	batch yagocrawlcontract.IngestBatch,
	_ yagocrawlcontract.CrawlProfile,
) error {
	f.mu.Lock()
	f.active++
	if f.active > f.maximum {
		f.maximum = f.active
	}
	f.seen = append(f.seen, batch.Document.NormalizedURL)
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
		return fmt.Errorf("context: %w", ctx.Err())
	case <-time.After(15 * time.Millisecond):
	}
	f.directory.store(documentstore.Document{
		NormalizedURL: batch.Document.NormalizedURL,
		ContentHash:   batch.Document.ContentHash,
	})
	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	return nil
}

func TestDocumentImportUsesBoundedConcurrentIngestAndSerializesDuplicateURLs(t *testing.T) {
	documents := &concurrentImportDirectory{documents: make(map[string]documentstore.Document)}
	submitter := &concurrentImportSubmitterFixture{directory: documents, limit: 4}
	service := &documentImportService{
		documents: documents,
		ingest:    submitter,
		frontier:  new(importScheduleFixture),
		profile:   importTestProfile(),
	}
	const duplicateURL = "https://same.example/page"
	records := []adminui.IndexImportRecord{
		importTestRecord("a", duplicateURL, "same", nil),
		importTestRecord("b", "https://b.example/page", "b", nil),
		importTestRecord("c", "https://c.example/page", "c", nil),
		importTestRecord("d", duplicateURL, "same", nil),
		importTestRecord("e", "https://e.example/page", "e", nil),
		importTestRecord("f", "https://f.example/page", "f", nil),
	}
	results, err := service.ImportDocuments(t.Context(), records)
	if err != nil {
		t.Fatalf("ImportDocuments: %v", err)
	}
	if len(results) != len(records) || results[0].Status != "imported" ||
		results[3].Status != "already-present" {
		t.Fatalf("ordered import results = %#v", results)
	}
	submitter.mu.Lock()
	maximum, submitted := submitter.maximum, len(submitter.seen)
	submitter.mu.Unlock()
	if maximum < 2 || maximum > submitter.limit {
		t.Fatalf("maximum concurrent submissions = %d, want 2..%d", maximum, submitter.limit)
	}
	if submitted != len(records)-1 {
		t.Fatalf("submitted %d records, want duplicate URL submitted once", submitted)
	}
}

func TestDocumentImportUsesBatchedDestinationSnapshotAndPreservesDuplicates(t *testing.T) {
	const (
		presentURL  = "https://present.example/page"
		conflictURL = "https://conflict.example/page"
		newURL      = "https://new.example/page"
		otherURL    = "https://other.example/page"
	)
	documents := &batchImportDocumentDirectory{
		documents: map[string]documentstore.Document{
			presentURL:  {NormalizedURL: presentURL, ContentHash: "same"},
			conflictURL: {NormalizedURL: conflictURL, ContentHash: "destination"},
		},
	}
	submitter := &batchImportSubmitterFixture{directory: documents}
	service := &documentImportService{
		documents: documents,
		ingest:    submitter,
		frontier:  new(importScheduleFixture),
		profile:   importTestProfile(),
	}
	results, err := service.ImportDocuments(t.Context(), []adminui.IndexImportRecord{
		importTestRecord("present", presentURL, "same", nil),
		importTestRecord("conflict", conflictURL, "source", nil),
		importTestRecord("new-first", newURL, "first", nil),
		importTestRecord("new-repeat", newURL, "first", nil),
		importTestRecord("new-conflict", newURL, "second", nil),
		importTestRecord("other", otherURL, "other", nil),
	})
	if err != nil {
		t.Fatalf("ImportDocuments: %v", err)
	}
	wantStatuses := []string{
		"already-present", "conflict", "imported", "already-present", "conflict", "imported",
	}
	if len(results) != len(wantStatuses) {
		t.Fatalf("results length = %d, want %d", len(results), len(wantStatuses))
	}
	for index, want := range wantStatuses {
		if results[index].Status != want {
			t.Errorf("result %d status = %q, want %q", index, results[index].Status, want)
		}
	}
	if got := len(documents.batchURLs); got != 4 {
		t.Fatalf("batched destination lookup URLs = %d, want 4 unique URLs", got)
	}
	if documents.lookups != 0 {
		t.Fatalf(
			"performed %d per-document lookup transactions after batch read",
			documents.lookups,
		)
	}
	if len(submitter.batches) != 2 {
		t.Fatalf("submitted %d records, want only the two unique new URLs", len(submitter.batches))
	}
	if documents.documents[conflictURL].ContentHash != "destination" {
		t.Fatal("conflicting import changed the pre-existing destination document")
	}
}

func TestDocumentImportRejectsIncompleteDestinationSnapshot(t *testing.T) {
	documents := &incompleteBatchImportDocumentDirectory{
		batchImportDocumentDirectory: &batchImportDocumentDirectory{
			documents: make(map[string]documentstore.Document),
		},
	}
	service := &documentImportService{
		documents: documents,
		ingest:    new(importSubmitterFixture),
		frontier:  new(importScheduleFixture),
		profile:   importTestProfile(),
	}
	if _, err := service.ImportDocuments(t.Context(), []adminui.IndexImportRecord{
		importTestRecord("row", "https://snapshot.example/", "hash", nil),
	}); err == nil {
		t.Fatal("incomplete destination snapshot was accepted")
	}
}

func TestDocumentImportReturnsEmptyResultsForEmptyBatch(t *testing.T) {
	profiles := new(importScheduleFixture)
	service := &documentImportService{
		documents: importDocumentDirectory{},
		ingest:    new(importSubmitterFixture),
		frontier:  profiles,
		profile:   importTestProfile(),
	}
	results, err := service.ImportDocuments(t.Context(), nil)
	if err != nil || len(results) != 0 {
		t.Fatalf("empty import = %#v, %v", results, err)
	}
	if len(profiles.profiles) != 1 {
		t.Fatalf("migration profiles registered = %d, want one", len(profiles.profiles))
	}
}

func TestDocumentImportStopsWhenBatchedDestinationReadFails(t *testing.T) {
	documents := &batchImportDocumentDirectory{
		documents: map[string]documentstore.Document{},
		batchErr:  errors.New("destination read unavailable"),
	}
	submitter := &batchImportSubmitterFixture{directory: documents}
	service := &documentImportService{
		documents: documents,
		ingest:    submitter,
		frontier:  new(importScheduleFixture),
		profile:   importTestProfile(),
	}
	if _, err := service.ImportDocuments(t.Context(), []adminui.IndexImportRecord{
		importTestRecord("row", "https://read-failure.example/", "hash", nil),
	}); err == nil {
		t.Fatal("ImportDocuments succeeded after the batched destination read failed")
	}
	if len(submitter.batches) != 0 {
		t.Fatalf(
			"submitted %d documents after destination preflight failed",
			len(submitter.batches),
		)
	}
}

type importScheduleFixture struct {
	urls          []string
	profiles      []string
	profileErr    error
	scheduleError error
}

type batchImportScheduleFixture struct {
	*importScheduleFixture
	batches [][]recrawlfrontier.RecrawlSchedule
}

func (f *batchImportScheduleFixture) ScheduleRecrawlsAtIfAbsent(
	_ context.Context,
	_ string,
	schedules []recrawlfrontier.RecrawlSchedule,
	_ time.Duration,
) error {
	if f.scheduleError != nil {
		return f.scheduleError
	}
	f.batches = append(f.batches, append([]recrawlfrontier.RecrawlSchedule(nil), schedules...))
	for _, schedule := range schedules {
		f.urls = append(f.urls, schedule.URL)
	}
	return nil
}

func (f *importScheduleFixture) RecordProfile(
	_ context.Context,
	profile yagocrawlcontract.CrawlProfile,
	_ yagocrawlcontract.CrawlOrderPriority,
) error {
	f.profiles = append(f.profiles, profile.Handle)
	return f.profileErr
}

func (f *importScheduleFixture) ScheduleAtIfAbsent(
	_ context.Context,
	url, _ string,
	_ time.Time,
	_ time.Duration,
) (bool, error) {
	f.urls = append(f.urls, url)
	return true, f.scheduleError
}

func TestDocumentImportDeduplicatesReplayAndPreservesConflictingDestination(t *testing.T) {
	const (
		presentURL   = "https://existing.example/page"
		conflictURL  = "https://conflict.example/page"
		canonicalURL = "https://canonical.example/page"
		mixedURL     = "https://mixed.example/page"
		newURL       = "https://new.example/page"
	)
	directory := importDocumentDirectory{
		presentURL: {NormalizedURL: presentURL, ContentHash: "same"},
		conflictURL: {
			NormalizedURL: conflictURL,
			ContentHash:   "destination",
		},
		canonicalURL: {
			NormalizedURL: canonicalURL,
			ContentHash:   "canonical-destination",
		},
		mixedURL: {NormalizedURL: mixedURL, ContentHash: "matching"},
		"https://mixed-canonical.example/page": {
			NormalizedURL: "https://mixed-canonical.example/page",
			ContentHash:   "canonical-destination",
		},
	}
	submitter := &importSubmitterFixture{docs: directory}
	schedules := new(importScheduleFixture)
	service := &documentImportService{
		documents: directory,
		ingest:    submitter,
		frontier:  schedules,
		profile:   importTestProfile(),
	}
	canonicalConflict := importTestRecord(
		"canonical-conflict",
		"https://alias.example/page",
		"source",
		nil,
	)
	canonicalConflict.Document.CanonicalURL = canonicalURL
	mixedConflict := importTestRecord("mixed-conflict", mixedURL, "matching", nil)
	mixedConflict.Document.CanonicalURL = "https://mixed-canonical.example/page"
	results, err := service.ImportDocuments(t.Context(), []adminui.IndexImportRecord{
		importTestRecord("existing", presentURL, "same", nil),
		importTestRecord("conflict", conflictURL, "source", nil),
		importTestRecord("new", newURL, "new-hash", nil),
		canonicalConflict,
		mixedConflict,
	})
	if err != nil {
		t.Fatalf("ImportDocuments: %v", err)
	}
	if len(results) != 5 || results[0].Status != "already-present" ||
		results[1].Status != "conflict" || results[2].Status != "imported" ||
		results[3].Status != "imported" || results[4].Status != "already-present" {
		t.Fatalf("results = %#v", results)
	}
	if len(submitter.batches) != 2 || submitter.batches[0].SourceURL != newURL ||
		submitter.batches[1].SourceURL != "https://alias.example/page" {
		t.Fatalf("submitted batches = %#v", submitter.batches)
	}
	if directory[conflictURL].ContentHash != "destination" {
		t.Fatal("conflicting import changed the existing destination document")
	}
	if directory[canonicalURL].ContentHash != "canonical-destination" {
		t.Fatal("alias import changed the existing canonical destination document")
	}
	if len(schedules.urls) != 0 {
		t.Fatalf("unscheduled documents entered the recrawl frontier: %#v", schedules.urls)
	}
	if len(schedules.profiles) != 1 {
		t.Fatalf("migration profile registrations = %d, want exactly one", len(schedules.profiles))
	}
}

func TestDocumentImportSchedulesOnlyExplicitVisitAndReplaysIdempotently(t *testing.T) {
	const url = "https://scheduled.example/page"
	nextVisit := time.Now().UTC().Add(-time.Minute)
	documents := importDocumentDirectory{}
	submitter := &importSubmitterFixture{docs: documents}
	service := &documentImportService{
		documents: documents,
		ingest:    submitter,
		frontier:  new(importScheduleFixture),
		profile:   importTestProfile(),
	}
	record := importTestRecord("row-1", url, "hash-1", &nextVisit)
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := service.ImportDocuments(
			t.Context(),
			[]adminui.IndexImportRecord{record},
		); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if got := len(service.frontier.(*importScheduleFixture).urls); got != 2 {
		t.Fatalf(
			"schedule attempts = %d, want idempotent scheduling attempted on both retries",
			got,
		)
	}
	if service.frontier.(*importScheduleFixture).urls[0] != url {
		t.Fatal("unexpected scheduled URL")
	}
	if len(submitter.batches) != 1 {
		t.Fatalf("replayed import submitted %d documents, want 1", len(submitter.batches))
	}
	if got := len(service.frontier.(*importScheduleFixture).profiles); got != 1 {
		t.Fatalf("profile registration attempts = %d, want one", got)
	}
}

func TestDocumentImportSchedulesEligibleRecrawlsOncePerImportBatch(t *testing.T) {
	const (
		presentURL  = "https://already.example/page"
		conflictURL = "https://conflict.example/page"
		newURL      = "https://new.example/page"
	)
	documents := importDocumentDirectory{
		presentURL:  {NormalizedURL: presentURL, ContentHash: "same"},
		conflictURL: {NormalizedURL: conflictURL, ContentHash: "destination"},
	}
	schedules := &batchImportScheduleFixture{importScheduleFixture: new(importScheduleFixture)}
	service := &documentImportService{
		documents: documents,
		ingest:    &importSubmitterFixture{docs: documents},
		frontier:  schedules,
		profile:   importTestProfile(),
	}
	visit := time.Now().UTC().Add(time.Hour)
	results, err := service.ImportDocuments(t.Context(), []adminui.IndexImportRecord{
		importTestRecord("same", presentURL, "same", &visit),
		importTestRecord("new", newURL, "new", &visit),
		importTestRecord("conflict", conflictURL, "source", &visit),
		importTestRecord("no-schedule", "https://unscheduled.example/page", "other", nil),
	})
	if err != nil {
		t.Fatalf("ImportDocuments: %v", err)
	}
	if results[0].Status != "already-present" || results[1].Status != "imported" ||
		results[2].Status != "conflict" || results[3].Status != "imported" {
		t.Fatalf("import results = %#v", results)
	}
	if len(schedules.batches) != 1 || len(schedules.batches[0]) != 2 {
		t.Fatalf("recrawl schedule batches = %#v", schedules.batches)
	}
	if schedules.batches[0][0].URL != presentURL || schedules.batches[0][1].URL != newURL {
		t.Fatalf("recrawl schedules = %#v", schedules.batches[0])
	}
	if documents[conflictURL].ContentHash != "destination" {
		t.Fatal("conflicting source record replaced the destination document")
	}
}

func TestDocumentImportReturnsBulkScheduleFailureForIdempotentRetry(t *testing.T) {
	const url = "https://schedule-retry.example/page"
	documents := importDocumentDirectory{}
	schedules := &batchImportScheduleFixture{
		importScheduleFixture: &importScheduleFixture{
			scheduleError: errors.New("frontier unavailable"),
		},
	}
	service := &documentImportService{
		documents: documents,
		ingest:    &importSubmitterFixture{docs: documents},
		frontier:  schedules,
		profile:   importTestProfile(),
	}
	visit := time.Now().UTC().Add(time.Hour)
	results, err := service.ImportDocuments(t.Context(), []adminui.IndexImportRecord{
		importTestRecord("retry-row", url, "hash", &visit),
	})
	if err == nil || len(results) != 0 {
		t.Fatalf("ImportDocuments = %#v, %v; want retryable schedule failure", results, err)
	}
	if documents[url].ContentHash != "hash" {
		t.Fatal("document ingest was rolled back after recrawl scheduling failed")
	}
	if len(schedules.batches) != 0 {
		t.Fatalf("failed schedule batches = %#v", schedules.batches)
	}
}

func TestDocumentImportReturnsIngestFailureWithoutAdvancingCaller(t *testing.T) {
	submitter := &importSubmitterFixture{
		docs: importDocumentDirectory{},
		err:  errors.New("storage unavailable"),
	}
	service := &documentImportService{
		documents: importDocumentDirectory{},
		ingest:    submitter,
		frontier:  new(importScheduleFixture),
		profile:   importTestProfile(),
	}
	if _, err := service.ImportDocuments(t.Context(), []adminui.IndexImportRecord{
		importTestRecord("row-1", "https://failure.example/", "hash", nil),
	}); err == nil {
		t.Fatal("ImportDocuments succeeded after the ingest path failed")
	}
	if len(submitter.batches) != 1 {
		t.Fatalf("submitted batches = %d, want 1", len(submitter.batches))
	}
}

func TestDocumentImportReportsProfileLookupAndRegistrationFailures(t *testing.T) {
	t.Run("profile registration", func(t *testing.T) {
		service := &documentImportService{
			documents: importDocumentDirectory{},
			ingest:    new(importSubmitterFixture),
			frontier:  &importScheduleFixture{profileErr: errors.New("profile store unavailable")},
			profile:   importTestProfile(),
		}
		if _, err := service.ImportDocuments(t.Context(), nil); err == nil {
			t.Fatal("profile registration failure was hidden")
		}
	})
	t.Run("existing document lookup", func(t *testing.T) {
		service := &documentImportService{
			documents: failingImportDocumentDirectory{
				err: errors.New("document store unavailable"),
			},
			ingest:   new(importSubmitterFixture),
			frontier: new(importScheduleFixture),
			profile:  importTestProfile(),
		}
		if _, err := service.ImportDocuments(t.Context(), []adminui.IndexImportRecord{
			importTestRecord("row", "https://lookup.example/", "hash", nil),
		}); err == nil {
			t.Fatal("destination lookup failure was hidden")
		}
	})
}

func TestDocumentImportHonorsCancellationBeforeEachRecord(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	service := &documentImportService{
		documents:       importDocumentDirectory{},
		ingest:          new(importSubmitterFixture),
		frontier:        new(importScheduleFixture),
		profile:         importTestProfile(),
		profileRecorded: true,
	}
	if _, err := service.ImportDocuments(ctx, []adminui.IndexImportRecord{
		importTestRecord("row", "https://cancel.example/", "hash", nil),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ImportDocuments error = %v, want context canceled", err)
	}
}

func TestDocumentImportReportsCancellationAfterIngestCompletes(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	service := &documentImportService{
		documents:       importDocumentDirectory{},
		ingest:          cancelingImportSubmitter{cancel: cancel},
		frontier:        new(importScheduleFixture),
		profile:         importTestProfile(),
		profileRecorded: true,
	}
	if _, err := service.ImportDocuments(ctx, []adminui.IndexImportRecord{
		importTestRecord("row", "https://cancel-after-ingest.example/", "hash", nil),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ImportDocuments error = %v, want context canceled", err)
	}
}

func TestImportDocumentRejectsCanceledContextBeforeLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	submitter := new(importSubmitterFixture)
	service := &documentImportService{ingest: submitter, profile: importTestProfile()}
	_, _, err := service.importDocument(ctx, importTestRecord(
		"canceled", "https://cancel.example/", "hash", nil,
	), importDestination{known: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("importDocument error = %v, want context canceled", err)
	}
	if len(submitter.batches) != 0 {
		t.Fatalf("canceled import submitted %d batches", len(submitter.batches))
	}
}

func TestDocumentImportDoesNotScheduleConflictingDestination(t *testing.T) {
	service := &documentImportService{
		frontier: new(importScheduleFixture),
		profile:  importTestProfile(),
	}
	visit := time.Now().UTC().Add(time.Hour)
	if err := service.scheduleImportBatch(t.Context(), []adminui.IndexImportRecord{
		importTestRecord("eligible", "https://eligible.example/", "matching", &visit),
		importTestRecord("conflict", "https://conflict.example/", "source", &visit),
	}, []adminui.IndexImportResult{
		{ID: "eligible", Status: "imported"},
		{ID: "conflict", Status: "conflict"},
	}); err != nil {
		t.Fatalf("schedule conflicting import: %v", err)
	}
	if got := service.frontier.(*importScheduleFixture).urls; len(got) != 1 ||
		got[0] != "https://eligible.example/" {
		t.Fatalf("scheduled documents = %#v, want only the eligible URL", got)
	}
}

func TestDocumentImportReportsRecrawlScheduleFailures(t *testing.T) {
	tests := []struct {
		name      string
		document  documentstore.Document
		content   string
		wantCalls int
	}{
		{
			name: "already present",
			document: documentstore.Document{
				NormalizedURL: "https://scheduled-existing.example/",
				ContentHash:   "same",
			},
			content: "same",
		},
		{name: "new import", content: "new", wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const url = "https://scheduled-existing.example/"
			documents := importDocumentDirectory{}
			if test.document.NormalizedURL != "" {
				documents[url] = test.document
			}
			submitter := &importSubmitterFixture{docs: documents}
			schedule := &importScheduleFixture{scheduleError: errors.New("frontier unavailable")}
			service := &documentImportService{
				documents: documents,
				ingest:    submitter,
				frontier:  schedule,
				profile:   importTestProfile(),
			}
			nextVisit := time.Now().UTC().Add(time.Hour)
			_, err := service.ImportDocuments(t.Context(), []adminui.IndexImportRecord{
				importTestRecord("row", url, test.content, &nextVisit),
			})
			if err == nil {
				t.Fatal("recrawl schedule failure was hidden")
			}
			if got := len(submitter.batches); got != test.wantCalls {
				t.Fatalf("submitted batches = %d, want %d", got, test.wantCalls)
			}
			if len(schedule.urls) != 1 {
				t.Fatalf("schedule attempts = %d, want one", len(schedule.urls))
			}
		})
	}
}

func TestCrawlRuntimeImportDocumentsDelegatesResultsAndErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "failure", err: errors.New("fixture ingest failure")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			submitter := &importSubmitterFixture{err: test.err}
			service := &documentImportService{
				documents: importDocumentDirectory{},
				ingest:    submitter,
				frontier:  new(importScheduleFixture),
				profile:   importTestProfile(),
			}
			runtime := &crawlRuntime{indexImport: service}
			results, err := runtime.ImportDocuments(t.Context(), []adminui.IndexImportRecord{
				importTestRecord("runtime-row", "https://runtime.example/", "hash", nil),
			})
			if test.err != nil {
				if err == nil || len(results) != 0 {
					t.Fatalf("ImportDocuments = %+v, %v; want wrapped error", results, err)
				}
				return
			}
			if err != nil || len(results) != 1 || results[0].Status != "imported" {
				t.Fatalf("ImportDocuments = %+v, %v", results, err)
			}
		})
	}
}

func importTestProfile() yagocrawlcontract.CrawlProfile {
	return yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{Name: "migration-test"})
}

func TestNewDocumentImportServiceConfiguresAnImportOnlyProfile(t *testing.T) {
	documents := importDocumentDirectory{}
	service := newDocumentImportService(documents, nil, nil)
	if service.documents == nil || service.profile.Name != "Document migration recrawl" ||
		service.profile.Scope != yagocrawlcontract.ScopeWide ||
		service.profile.URLMustMatch != yagocrawlcontract.MatchAll ||
		service.profile.MaxPagesPerHost != 1 || !service.profile.AllowQueryURLs {
		t.Fatalf("migration service profile = %+v", service.profile)
	}
	if ingest, ok := service.ingest.(*crawlbroker.IngestReceiver); !ok || ingest != nil {
		t.Fatal("constructor replaced the supplied ingest dependency")
	}
	if frontier, ok := service.frontier.(*recrawlfrontier.Frontier); !ok || frontier != nil {
		t.Fatal("constructor replaced the supplied frontier dependency")
	}
}

func importTestRecord(id, url, contentHash string, nextVisit *time.Time) adminui.IndexImportRecord {
	return adminui.IndexImportRecord{
		ID:          id,
		NextVisitAt: nextVisit,
		Document: yagocrawlcontract.DocumentIngest{
			CanonicalURL:  url,
			NormalizedURL: url,
			ContentHash:   contentHash,
			ExtractedText: "fixture text",
			FetchedAt:     time.Now().UTC(),
		},
	}
}
