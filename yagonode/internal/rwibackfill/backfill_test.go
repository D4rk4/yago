package rwibackfill

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/shardvault"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type documentSequence struct {
	documents []documentstore.Document
}

func (s documentSequence) ScanStoredDocumentPagesFrom(
	ctx context.Context,
	cursor string,
	visit func(*documentstore.Document, string, bool) (bool, error),
) error {
	start := 0
	if cursor != "" {
		var err error
		start, err = strconv.Atoi(cursor)
		if err != nil {
			return fmt.Errorf("parse document cursor: %w", err)
		}
	}
	for index := start; index < len(s.documents); index++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("document scan context: %w", err)
		}
		document := s.documents[index]
		keepGoing, err := visit(&document, strconv.Itoa(index+1), index == len(s.documents)-1)
		if err != nil || !keepGoing {
			return err
		}
	}
	if len(s.documents) == 0 || start >= len(s.documents) {
		_, err := visit(nil, strconv.Itoa(len(s.documents)), true)
		return err
	}
	return nil
}

type failingDocumentSequence struct {
	err        error
	noBoundary bool
}

func (s failingDocumentSequence) ScanStoredDocumentPagesFrom(
	context.Context,
	string,
	func(*documentstore.Document, string, bool) (bool, error),
) error {
	if s.noBoundary {
		return nil
	}
	return s.err
}

type urlRows struct {
	rows       map[yagomodel.URLHash]yagomodel.URIMetadataRow
	newRows    int
	busyCount  int
	receiveErr error
	reject     bool
}

func (r *urlRows) Receive(
	_ context.Context,
	rows []yagomodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	if r.receiveErr != nil {
		return urlmeta.Receipt{}, r.receiveErr
	}
	if r.busyCount > 0 {
		r.busyCount--
		return urlmeta.Receipt{Busy: true}, nil
	}
	for _, row := range rows {
		hash, err := row.URLHash()
		if err != nil {
			return urlmeta.Receipt{}, fmt.Errorf("hash test URL metadata: %w", err)
		}
		if _, exists := r.rows[hash]; exists {
			continue
		}
		if r.reject {
			return urlmeta.Receipt{ErrorURL: []yagomodel.Hash{hash.Hash()}}, nil
		}
		r.rows[hash] = row
		r.newRows++
	}
	return urlmeta.Receipt{}, nil
}

type postingRows struct {
	urls        *urlRows
	rows        map[string]yagomodel.RWIPosting
	failCount   int
	receiveErr  error
	unknownURL  bool
	busyCount   int
	busySignal  chan struct{}
	busyRelease <-chan struct{}
	calls       int
	maxBatch    int
}

func (r *postingRows) Receive(
	_ context.Context,
	postings []yagomodel.RWIPosting,
) (rwi.Receipt, error) {
	r.calls++
	r.maxBatch = max(r.maxBatch, len(postings))
	if r.receiveErr != nil {
		return rwi.Receipt{}, r.receiveErr
	}
	if r.busyCount > 0 {
		r.busyCount--
		if r.busySignal != nil {
			r.busySignal <- struct{}{}
		}
		if r.busyRelease != nil {
			<-r.busyRelease
		}
		return rwi.Receipt{Busy: true}, nil
	}
	if r.failCount > 0 {
		r.failCount--
		return rwi.Receipt{}, errors.New("scripted write interruption")
	}
	for _, posting := range postings {
		urlHash, err := posting.URLHash()
		if err != nil {
			return rwi.Receipt{}, fmt.Errorf("hash test posting URL: %w", err)
		}
		if _, exists := r.urls.rows[urlHash]; !exists {
			return rwi.Receipt{UnknownURL: []yagomodel.Hash{urlHash.Hash()}}, nil
		}
		if r.unknownURL {
			return rwi.Receipt{UnknownURL: []yagomodel.Hash{urlHash.Hash()}}, nil
		}
		r.rows[string(posting.WordHash)+":"+urlHash.String()] = posting
	}
	return rwi.Receipt{}, nil
}

func openBackfillFixture(
	t *testing.T,
	documents []documentstore.Document,
) (*Backfill, *urlRows, *postingRows) {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	urls := &urlRows{rows: make(map[yagomodel.URLHash]yagomodel.URIMetadataRow)}
	postings := &postingRows{urls: urls, rows: make(map[string]yagomodel.RWIPosting)}
	backfill, err := Open(storage, documentSequence{documents: documents}, urls, postings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	backfill.clock = func() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) }
	return backfill, urls, postings
}

func importedDocument(url, status string) documentstore.Document {
	return documentstore.Document{
		NormalizedURL: url,
		CanonicalURL:  url,
		FetchStatus:   status,
		Title:         "A useful migration title",
		ExtractedText: "A useful migration document with repeated migration terms.",
		Language:      "en",
		FetchedAt:     time.Date(2024, 3, 4, 5, 6, 0, 0, time.UTC),
		ModifiedAt:    time.Date(2024, 2, 3, 0, 0, 0, 0, time.UTC),
		Metadata:      map[string]string{"yacy.fresh_until": "2024-04-05T00:00:00Z"},
		Outlinks:      []string{url + "child", "https://other.test/"},
	}
}

func runOnce(t *testing.T, backfill *Backfill, state checkpoint) {
	t.Helper()
	backfill.execute(t.Context(), state)
}

func TestBackfillCompletesSuccessfulRowsAndSkipsUnusableRows(t *testing.T) {
	backfill, urls, postings := openBackfillFixture(t, []documentstore.Document{
		importedDocument("https://example.test/one", "200"),
		importedDocument("https://example.test/redirect", "302"),
		importedDocument("file:///unsupported", "200"),
	})
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, found, err := backfill.repository.load(t.Context())
	if err != nil || !found {
		t.Fatalf("load state: found=%v err=%v", found, err)
	}
	runOnce(t, backfill, state)
	status, err := backfill.Status(t.Context())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != stateComplete || status.ScannedDocuments != 3 ||
		status.IndexedDocuments != 2 || status.SkippedDocuments != 1 {
		t.Fatalf("status = %+v", status)
	}
	if len(urls.rows) != 2 || len(postings.rows) == 0 {
		t.Fatalf("stored URL rows=%d posting rows=%d", len(urls.rows), len(postings.rows))
	}
	if status.EstimatedBytes == 0 || status.PostingRows != int64(len(postings.rows)) {
		t.Fatalf("status did not report generated storage: %+v", status)
	}
}

func TestBackfillPilotPausesAtLimitAndResumesFromCursor(t *testing.T) {
	backfill, urls, postings := openBackfillFixture(t, []documentstore.Document{
		importedDocument("https://example.test/one", "200"),
		importedDocument("https://example.test/two", "200"),
	})
	if _, err := backfill.Start(t.Context(), 1, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, _, _ := backfill.repository.load(t.Context())
	runOnce(t, backfill, state)
	status, _ := backfill.Status(t.Context())
	if status.State != statePaused || status.ScannedDocuments != 1 || len(urls.rows) != 1 {
		t.Fatalf("pilot status = %+v, URL rows=%d", status, len(urls.rows))
	}
	if _, err := backfill.Resume(t.Context(), 0, false); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	state, _, _ = backfill.repository.load(t.Context())
	runOnce(t, backfill, state)
	status, _ = backfill.Status(t.Context())
	if status.State != stateComplete || status.ScannedDocuments != 2 || len(urls.rows) != 2 {
		t.Fatalf("resumed status = %+v, URL rows=%d", status, len(urls.rows))
	}
	if len(postings.rows) == 0 {
		t.Fatal("resume produced no postings")
	}
}

func TestBackfillCanRepeatCompletedScanAfterMoreDocumentsArrive(t *testing.T) {
	documents := documentSequence{documents: []documentstore.Document{
		importedDocument("https://example.test/one", "200"),
	}}
	backfill, urls, postings := openBackfillFixture(t, documents.documents)
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, _, _ := backfill.repository.load(t.Context())
	runOnce(t, backfill, state)
	if status, _ := backfill.Status(t.Context()); status.State != stateComplete {
		t.Fatalf("first scan status = %+v", status)
	}
	documents.documents = append(
		documents.documents,
		importedDocument("https://example.test/two", "200"),
	)
	backfill.documents = documents
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start after completion: %v", err)
	}
	state, _, _ = backfill.repository.load(t.Context())
	runOnce(t, backfill, state)
	status, _ := backfill.Status(t.Context())
	if status.State != stateComplete || status.ScannedDocuments != 2 ||
		status.IndexedDocuments != 2 || len(urls.rows) != 2 {
		t.Fatalf(
			"second scan status=%+v URL rows=%d postings=%d",
			status,
			len(urls.rows),
			len(postings.rows),
		)
	}
}

func TestBackfillPauseBeforeNextDocumentDoesNotAdvancePastIt(t *testing.T) {
	backfill, urls, _ := openBackfillFixture(t, []documentstore.Document{
		importedDocument("https://example.test/one", "200"),
	})
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, _, _ := backfill.repository.load(t.Context())
	backfill.pause.Store(true)
	runOnce(t, backfill, state)
	paused, _ := backfill.Status(t.Context())
	if paused.State != statePaused || paused.ScannedDocuments != 0 || len(urls.rows) != 0 {
		t.Fatalf("pause advanced unprocessed document: status=%+v urls=%d", paused, len(urls.rows))
	}
	if _, err := backfill.Resume(t.Context(), 0, false); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	state, _, _ = backfill.repository.load(t.Context())
	runOnce(t, backfill, state)
	completed, _ := backfill.Status(t.Context())
	if completed.State != stateComplete || completed.ScannedDocuments != 1 || len(urls.rows) != 1 {
		t.Fatalf("resume status=%+v URL rows=%d", completed, len(urls.rows))
	}
}

func TestBackfillPauseInterruptsResourceWait(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, nil)
	backfill.pause.Store(true)
	if err := backfill.waitForRetry(t.Context(), 0); !errors.Is(err, errBackfillPaused) {
		t.Fatalf("resource wait error = %v, want pause", err)
	}
}

func TestBackfillRunReturnsWhenAlreadyCanceled(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	backfill.Run(ctx)
}

func TestBackfillOpenRequiresDependencies(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	defer func() { _ = storage.Close() }()
	urls := &urlRows{rows: make(map[yagomodel.URLHash]yagomodel.URIMetadataRow)}
	postings := &postingRows{urls: urls, rows: make(map[string]yagomodel.RWIPosting)}
	for name, scanner := range map[string]documentScanner{
		"storage":          nil,
		"scanner":          documentSequence{},
		"url receiver":     documentSequence{},
		"posting receiver": documentSequence{},
	} {
		candidateStorage := storage
		candidateScanner := scanner
		candidateURLs := urlmeta.URLReceiver(urls)
		candidatePostings := rwi.PostingReceiver(postings)
		switch name {
		case "storage":
			candidateStorage = nil
		case "scanner":
			candidateScanner = nil
		case "url receiver":
			candidateURLs = nil
		case "posting receiver":
			candidatePostings = nil
		}
		if _, err := Open(
			candidateStorage,
			candidateScanner,
			candidateURLs,
			candidatePostings,
		); err == nil {
			t.Fatalf("Open accepted missing %s", name)
		}
	}

	if _, err := Open(storage, documentSequence{}, urls, postings); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := Open(storage, documentSequence{}, urls, postings); err == nil {
		t.Fatal("Open accepted duplicate checkpoint registration")
	}
}

func TestBackfillRejectsInvalidRunBounds(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, nil)
	for _, maximum := range []int64{-1, 10_000_001} {
		if _, err := backfill.Start(t.Context(), maximum, false); err == nil {
			t.Fatalf("Start accepted maximum %d", maximum)
		}
		if _, err := backfill.Resume(t.Context(), maximum, false); err == nil {
			t.Fatalf("Resume accepted maximum %d", maximum)
		}
	}
}

func TestBackfillIdleAndActiveLifecycle(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, nil)
	if status, err := backfill.Pause(t.Context()); err != nil || status.State != stateIdle {
		t.Fatalf("Pause while idle = %+v, error %v", status, err)
	}
	if _, err := backfill.Resume(t.Context(), 0, false); err == nil {
		t.Fatal("Resume accepted missing checkpoint")
	}
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := backfill.Start(t.Context(), 0, false); err == nil {
		t.Fatal("Start replaced an active checkpoint")
	}
	if _, err := backfill.Pause(t.Context()); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	backfill.runPersistedCheckpoint(t.Context())
	if status, err := backfill.Status(t.Context()); err != nil || status.State != statePaused {
		t.Fatalf("persisted pause = %+v, error %v", status, err)
	}
	if _, err := backfill.Pause(t.Context()); err != nil {
		t.Fatalf("Pause while paused: %v", err)
	}
}

func TestBackfillResumeCompletesLifecycle(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, nil)
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := backfill.Pause(t.Context()); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	backfill.runPersistedCheckpoint(t.Context())
	if _, err := backfill.Resume(t.Context(), 0, false); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	state, _, err := backfill.repository.load(t.Context())
	if err != nil {
		t.Fatalf("load resumed state: %v", err)
	}
	backfill.execute(t.Context(), state)
	if _, err := backfill.Resume(t.Context(), 0, false); err == nil {
		t.Fatal("Resume accepted a completed checkpoint")
	}
}

func TestBackfillRunProcessesCheckpointThenWaitsForSignal(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, nil)
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	timer := time.AfterFunc(20*time.Millisecond, cancel)
	backfill.Run(ctx)
	if !timer.Stop() {
		<-time.After(time.Millisecond)
	}
	status, err := backfill.Status(t.Context())
	if err != nil || status.State != stateComplete {
		t.Fatalf("Run status = %+v, error %v", status, err)
	}
	if backfill.waitForSignal(ctx) {
		t.Fatal("waitForSignal accepted canceled context")
	}
	backfill.signal()
	if !backfill.waitForSignal(t.Context()) {
		t.Fatal("waitForSignal ignored a queued wake signal")
	}
}

func TestBackfillFlushPersistsPauseRequestedStateAndReportsSaveFailure(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, nil)
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := backfill.Pause(t.Context()); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	current, _, err := backfill.repository.load(t.Context())
	if err != nil {
		t.Fatalf("load pause request: %v", err)
	}
	current.DryRun = true
	pending := &batch{cursor: "resume-key", scanned: 1}
	if err := backfill.flush(t.Context(), &current, pending); err != nil {
		t.Fatalf("flush pause request: %v", err)
	}
	if current.State != statePaused || !backfill.pause.Load() {
		t.Fatalf(
			"pause request not persisted: checkpoint=%+v pause=%v",
			current,
			backfill.pause.Load(),
		)
	}

	engine := &checkpointTestEngine{}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	urls := &urlRows{rows: make(map[yagomodel.URLHash]yagomodel.URIMetadataRow)}
	postings := &postingRows{urls: urls, rows: make(map[string]yagomodel.RWIPosting)}
	failed, err := Open(storage, documentSequence{}, urls, postings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := failed.Start(t.Context(), 0, true); err != nil {
		t.Fatalf("Start save-failure fixture: %v", err)
	}
	state, _, err := failed.repository.load(t.Context())
	if err != nil {
		t.Fatalf("load save-failure checkpoint: %v", err)
	}
	engine.updateErr = errors.New("scripted checkpoint save failure")
	if err := failed.flush(t.Context(), &state, &batch{cursor: "next"}); err == nil {
		t.Fatal("flush ignored checkpoint save failure")
	}
}

func TestBackfillPersistedCheckpointHandlesMissingAndUnreadableState(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, nil)
	backfill.runPersistedCheckpoint(t.Context())
	if status, _ := backfill.Status(t.Context()); status.State != stateIdle {
		t.Fatalf("missing checkpoint status = %+v", status)
	}
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := backfill.repository.storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}
	backfill.runPersistedCheckpoint(t.Context())
	backfill.persistRequestedPause(t.Context(), checkpoint{})
	if status, err := backfill.Status(t.Context()); err == nil || status.State != "" {
		t.Fatalf("Status after storage close = %+v, error %v", status, err)
	}
	for name, operation := range map[string]func() error{
		"start":  func() error { _, err := backfill.Start(t.Context(), 0, false); return err },
		"pause":  func() error { _, err := backfill.Pause(t.Context()); return err },
		"resume": func() error { _, err := backfill.Resume(t.Context(), 0, false); return err },
	} {
		if err := operation(); err == nil {
			t.Errorf("%s ignored closed storage", name)
		}
	}
}

func TestBackfillLifecycleReportsCheckpointSaveFailures(t *testing.T) {
	engine := &checkpointTestEngine{}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	defer func() { _ = storage.Close() }()
	urls := &urlRows{rows: make(map[yagomodel.URLHash]yagomodel.URIMetadataRow)}
	postings := &postingRows{urls: urls, rows: make(map[string]yagomodel.RWIPosting)}
	backfill, err := Open(storage, documentSequence{}, urls, postings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	engine.updateErr = errors.New("start checkpoint write failed")
	if _, err := backfill.Start(t.Context(), 0, false); err == nil {
		t.Fatal("Start ignored checkpoint write failure")
	}
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start after failed write: %v", err)
	}
	engine.updateErr = errors.New("pause checkpoint write failed")
	if _, err := backfill.Pause(t.Context()); err == nil {
		t.Fatal("Pause ignored checkpoint write failure")
	}
	if _, err := backfill.Pause(t.Context()); err != nil {
		t.Fatalf("Pause after failed write: %v", err)
	}
	backfill.runPersistedCheckpoint(t.Context())
	engine.updateErr = errors.New("resume checkpoint write failed")
	if _, err := backfill.Resume(t.Context(), 0, false); err == nil {
		t.Fatal("Resume ignored checkpoint write failure")
	}
	if _, err := backfill.Resume(t.Context(), 0, false); err != nil {
		t.Fatalf("Resume after failed write: %v", err)
	}
}

func TestBackfillScannerFailuresAndIncompleteBoundariesFailCheckpoint(t *testing.T) {
	for name, documents := range map[string]documentScanner{
		"scan failure":     failingDocumentSequence{err: errors.New("scan interrupted")},
		"missing boundary": failingDocumentSequence{noBoundary: true},
	} {
		t.Run(name, func(t *testing.T) {
			backfill, _, _ := openBackfillFixture(t, nil)
			backfill.documents = documents
			if _, err := backfill.Start(t.Context(), 0, false); err != nil {
				t.Fatalf("Start: %v", err)
			}
			state, _, err := backfill.repository.load(t.Context())
			if err != nil {
				t.Fatalf("load checkpoint: %v", err)
			}
			backfill.execute(t.Context(), state)
			status, err := backfill.Status(t.Context())
			if err != nil || status.State != stateFailed || status.LastError == "" {
				t.Fatalf("failure status = %+v, error %v", status, err)
			}
		})
	}
}

func TestBackfillReceiverFailuresAndRejectionsFailWithoutAdvancing(t *testing.T) {
	cases := []struct {
		name      string
		configure func(*urlRows, *postingRows)
	}{
		{name: "metadata error", configure: func(urls *urlRows, _ *postingRows) {
			urls.receiveErr = errors.New("metadata receiver unavailable")
		}},
		{name: "metadata rejection", configure: func(urls *urlRows, _ *postingRows) {
			urls.reject = true
		}},
		{name: "posting error", configure: func(_ *urlRows, postings *postingRows) {
			postings.receiveErr = errors.New("posting receiver unavailable")
		}},
		{name: "unknown URL reference", configure: func(_ *urlRows, postings *postingRows) {
			postings.unknownURL = true
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			backfill, urls, postings := openBackfillFixture(t, []documentstore.Document{
				importedDocument("https://example.test/failure", "200"),
			})
			testCase.configure(urls, postings)
			if _, err := backfill.Start(t.Context(), 0, false); err != nil {
				t.Fatalf("Start: %v", err)
			}
			state, _, err := backfill.repository.load(t.Context())
			if err != nil {
				t.Fatalf("load checkpoint: %v", err)
			}
			backfill.execute(t.Context(), state)
			status, err := backfill.Status(t.Context())
			if err != nil || status.State != stateFailed || status.ScannedDocuments != 0 {
				t.Fatalf("failed receiver status = %+v, error %v", status, err)
			}
		})
	}
}

func TestBackfillWaitForRetryHandlesPauseWakeAndCancellation(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, nil)
	backfill.pause.Store(true)
	if err := backfill.waitForRetry(t.Context(), 0); !errors.Is(err, errBackfillPaused) {
		t.Fatalf("paused wait error = %v", err)
	}
	backfill.pause.Store(false)
	backfill.signal()
	if err := backfill.waitForRetry(t.Context(), 9); err != nil {
		t.Fatalf("woken retry error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := backfill.waitForRetry(canceled, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retry error = %v", err)
	}
	backfill.urls = &urlRows{
		rows: make(map[yagomodel.URLHash]yagomodel.URIMetadataRow), busyCount: 1,
	}
	if err := backfill.receiveURLMetadata(canceled, []yagomodel.URIMetadataRow{{}}); err == nil {
		t.Fatal("metadata retry ignored canceled context")
	}
	backfill.pause.Store(false)
	backfill.wake = make(chan struct{}, 1)
	backfill.pause.Store(true)
	backfill.wake <- struct{}{}
	if err := backfill.waitForRetrySignal(t.Context(), nil); !errors.Is(err, errBackfillPaused) {
		t.Fatalf("pause during retry error = %v", err)
	}
}

func TestBackfillPostingBackpressureAndAppendFailure(t *testing.T) {
	document := importedDocument("https://example.test/posting-busy", "200")
	backfill, _, postings := openBackfillFixture(t, []documentstore.Document{document})
	postings.busyCount = 1
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-backfill.wake
	state, _, err := backfill.repository.load(t.Context())
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	backfill.execute(t.Context(), state)
	if status, _ := backfill.Status(
		t.Context(),
	); status.State != stateComplete ||
		postings.calls < 2 {
		t.Fatalf("posting backpressure status=%+v calls=%d", status, postings.calls)
	}

	textFor := func(first int) string {
		var text strings.Builder
		for index := range 5000 {
			_, _ = fmt.Fprintf(&text, "batchword%08d ", first+index)
		}
		return text.String()
	}
	first := importedDocument("https://example.test/batch-one", "200")
	first.ExtractedText = textFor(0)
	second := importedDocument("https://example.test/batch-two", "200")
	second.ExtractedText = textFor(5000)
	failed, rejectedURLs, _ := openBackfillFixture(t, []documentstore.Document{first, second})
	rejectedURLs.receiveErr = errors.New("scripted metadata failure")
	if _, err := failed.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start append failure fixture: %v", err)
	}
	state, _, err = failed.repository.load(t.Context())
	if err != nil {
		t.Fatalf("load append failure checkpoint: %v", err)
	}
	failed.execute(t.Context(), state)
	if status, _ := failed.Status(t.Context()); status.State != stateFailed {
		t.Fatalf("append-triggered flush failure status = %+v", status)
	}
}

func TestBackfillPostingRetryStopsWhenPauseIsRequested(t *testing.T) {
	backfill, _, postings := openBackfillFixture(t, []documentstore.Document{
		importedDocument("https://example.test/posting-pause", "200"),
	})
	postings.busyCount = 1
	postings.busySignal = make(chan struct{}, 1)
	busyRelease := make(chan struct{})
	postings.busyRelease = busyRelease
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-backfill.wake
	backfill.wake = make(chan struct{})
	state, _, err := backfill.repository.load(t.Context())
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	done := make(chan struct{})
	go func() {
		backfill.execute(t.Context(), state)
		close(done)
	}()
	select {
	case <-postings.busySignal:
	case <-time.After(5 * time.Second):
		t.Fatal("posting receiver did not report backpressure")
	}
	if _, err := backfill.Pause(t.Context()); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	close(busyRelease)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("backfill did not stop after pause was requested")
	}
	status, err := backfill.Status(t.Context())
	if err != nil || status.State != statePaused || postings.calls != 1 {
		t.Fatalf("posting pause status=%+v calls=%d error=%v", status, postings.calls, err)
	}
}

func TestBackfillFlushNoopDryRunAndCheckpointStorageFailures(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, nil)
	state := checkpoint{
		Format: checkpointFormat, State: stateRunning,
		UpdatedAtUnixMilli: backfill.clock().UnixMilli(),
	}
	pending := &batch{scanned: 1}
	if err := backfill.flush(t.Context(), &state, pending); err != nil || pending.scanned != 1 {
		t.Fatalf("empty-cursor flush = %v, pending %+v", err, pending)
	}
	state.DryRun = true
	pending.cursor = "1"
	if err := backfill.flush(t.Context(), &state, pending); err != nil {
		t.Fatalf("dry-run flush: %v", err)
	}
	if state.Cursor != "1" || state.ScannedDocuments != 1 || pending.cursor != "" {
		t.Fatalf("dry-run checkpoint = %+v pending %+v", state, pending)
	}
	if err := backfill.repository.storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}
	pending.cursor = "2"
	if err := backfill.flush(t.Context(), &state, pending); err == nil {
		t.Fatal("flush succeeded with closed checkpoint storage")
	}
}

func TestBackfillPauseAndCanceledExecutionDoNotLoseCheckpoint(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, []documentstore.Document{
		importedDocument("https://example.test/pause", "200"),
	})
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, _, err := backfill.repository.load(t.Context())
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	backfill.pause.Store(true)
	backfill.execute(t.Context(), state)
	if status, _ := backfill.Status(t.Context()); status.State != statePaused {
		t.Fatalf("pause state = %+v", status)
	}
	if err := backfill.repository.save(t.Context(), checkpoint{
		Format: checkpointFormat, State: stateRunning,
		UpdatedAtUnixMilli: backfill.clock().UnixMilli(),
	}); err != nil {
		t.Fatalf("save running checkpoint: %v", err)
	}
	state, _, err = backfill.repository.load(t.Context())
	if err != nil {
		t.Fatalf("reload checkpoint: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	backfill.handleExecutionError(ctx, state, errors.New("canceled run"))
	if status, _ := backfill.Status(t.Context()); status.State != stateRunning {
		t.Fatalf("canceled execution altered checkpoint: %+v", status)
	}
	if err := backfill.pauseExecution(ctx, state, &batch{}); err == nil {
		t.Fatal("pauseExecution ignored canceled checkpoint persistence")
	}
	if err := backfill.pauseExecution(ctx, state, &batch{cursor: "next"}); err == nil {
		t.Fatal("pauseExecution ignored canceled flush")
	}
	backfill.handleExecutionError(t.Context(), state, errBackfillPaused)
	if status, _ := backfill.Status(t.Context()); status.State != statePaused {
		t.Fatalf("pause error state = %+v", status)
	}
	backfill.pause.Store(false)
	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	progress := scanProgress{checkpoint: &state, pending: &batch{}, processedThisRun: new(int64)}
	if _, err := backfill.visitBackfillDocument(
		ctx,
		progress,
		scanCandidate{cursor: "next"},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("canceled document visit error = %v", err)
	}
	if err := backfill.repository.storage.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}
	backfill.handleExecutionError(t.Context(), state, errors.New("persistence failure"))
}

func TestIndexableFetchStatus(t *testing.T) {
	for _, status := range []string{"", "fetched", "200", "299", "500", "bad"} {
		got := indexableFetchStatus(status)
		want := status == "" || status == "fetched" || status == "200" || status == "299"
		if got != want {
			t.Errorf("indexableFetchStatus(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestLinkCountsCompareHostsAndRejectMalformedSource(t *testing.T) {
	local, external := linkCounts("https://example.test/start", []string{
		"https://EXAMPLE.test/local", "https://other.test/", "relative", "://bad", "file:///nohost",
	})
	if local != 1 || external != 1 {
		t.Fatalf("linkCounts = (%d, %d)", local, external)
	}
	if local, external := linkCounts(
		"://bad",
		[]string{"https://other.test/"},
	); local != 0 ||
		external != 0 {
		t.Fatalf("linkCounts accepted malformed source: (%d, %d)", local, external)
	}
}

func TestImportedFreshnessAcceptsISODateAndRejectsInvalid(t *testing.T) {
	if got := importedFreshness("2024-04-05"); got.Format("2006-01-02") != "2024-04-05" {
		t.Fatalf("date-only freshness = %v", got)
	}
	if got := importedFreshness("not-a-date"); !got.IsZero() {
		t.Fatalf("invalid freshness = %v", got)
	}
}

func TestBackfillArtifactsUseFetchStatusAndIndexedTimestamp(t *testing.T) {
	if artifacts, ok := buildArtifacts(
		importedDocument("https://example.test/status", "fetched"),
	); !ok ||
		len(artifacts.Postings) == 0 {
		t.Fatal("buildArtifacts rejected fetched status")
	}
	documentWithIndexTime := importedDocument("https://example.test/indexed-time", "200")
	documentWithIndexTime.FetchedAt = time.Time{}
	documentWithIndexTime.IndexedAt = time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	artifacts, ok := buildArtifacts(documentWithIndexTime)
	if !ok || artifacts.Metadata.Properties["load"] != "20250102" {
		t.Fatalf("indexed timestamp not used for load metadata: %+v", artifacts.Metadata)
	}
}

func TestBackfillArtifactsRespectPostingLimit(t *testing.T) {
	var text strings.Builder
	for index := range yagocrawlcontract.MaximumIngestPostings + 1 {
		_, _ = fmt.Fprintf(&text, "unique%05d ", index)
	}
	document := importedDocument("https://example.test/posting-limit", "200")
	document.ExtractedText = text.String()
	artifacts, ok := buildArtifacts(document)
	if !ok || len(artifacts.Postings) != yagocrawlcontract.MaximumIngestPostings {
		t.Fatalf("posting limit artifacts accepted=%v rows=%d", ok, len(artifacts.Postings))
	}
}

func TestBackfillDoesNotResumeBeforePauseCheckpointSettles(t *testing.T) {
	backfill, _, _ := openBackfillFixture(t, nil)
	if _, err := backfill.Start(t.Context(), 1, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := backfill.Pause(t.Context()); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if _, err := backfill.Resume(t.Context(), 0, false); err == nil {
		t.Fatal("resume accepted a pause request that had not settled")
	}
}

func TestBackfillPostingReceivesStayWithinBatchLimit(t *testing.T) {
	textFor := func(first int) string {
		var text strings.Builder
		for index := range 5000 {
			_, _ = fmt.Fprintf(&text, "word%08d ", first+index)
		}
		return text.String()
	}
	first := importedDocument("https://example.test/one", "200")
	first.ExtractedText = textFor(0)
	second := importedDocument("https://example.test/two", "200")
	second.ExtractedText = textFor(5000)
	backfill, _, postings := openBackfillFixture(t, []documentstore.Document{first, second})
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, _, _ := backfill.repository.load(t.Context())
	runOnce(t, backfill, state)
	if postings.maxBatch > postingBatchLimit || postings.calls != 2 {
		t.Fatalf(
			"posting batches = %d calls, maximum %d rows; limit %d",
			postings.calls,
			postings.maxBatch,
			postingBatchLimit,
		)
	}
}

func TestBackfillCheckpointSurvivesControllerReopen(t *testing.T) {
	directory := t.TempDir()
	storage, err := shardvault.Open(directory, 0)
	if err != nil {
		t.Fatalf("shardvault.Open: %v", err)
	}
	urls := &urlRows{rows: make(map[yagomodel.URLHash]yagomodel.URIMetadataRow)}
	postings := &postingRows{urls: urls, rows: make(map[string]yagomodel.RWIPosting)}
	documents := documentSequence{documents: []documentstore.Document{
		importedDocument("https://example.test/one", "200"),
		importedDocument("https://example.test/two", "200"),
	}}
	backfill, err := Open(storage, documents, urls, postings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := backfill.Start(t.Context(), 1, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, _, _ := backfill.repository.load(t.Context())
	backfill.execute(t.Context(), state)
	if err := storage.Close(); err != nil {
		t.Fatalf("close first vault: %v", err)
	}
	reopenedStorage, err := shardvault.Open(directory, 0)
	if err != nil {
		t.Fatalf("reopen shardvault: %v", err)
	}
	defer func() {
		if err := reopenedStorage.Close(); err != nil {
			t.Errorf("close reopened shardvault: %v", err)
		}
	}()
	restarted, err := Open(reopenedStorage, documents, urls, postings)
	if err != nil {
		t.Fatalf("Open after restart: %v", err)
	}
	status, err := restarted.Status(t.Context())
	if err != nil || status.State != statePaused || status.ScannedDocuments != 1 {
		t.Fatalf("reopened checkpoint = %+v, error %v", status, err)
	}
	if _, err := restarted.Resume(t.Context(), 0, false); err != nil {
		t.Fatalf("Resume after restart: %v", err)
	}
	state, _, _ = restarted.repository.load(t.Context())
	restarted.execute(t.Context(), state)
	status, _ = restarted.Status(t.Context())
	if status.State != stateComplete || status.ScannedDocuments != 2 || len(urls.rows) != 2 {
		t.Fatalf("restarted backfill = %+v URL rows=%d", status, len(urls.rows))
	}
}

func TestBackfillDryRunDoesNotMutateRWIReceivers(t *testing.T) {
	backfill, urls, postings := openBackfillFixture(t, []documentstore.Document{
		importedDocument("https://example.test/one", "200"),
	})
	if _, err := backfill.Start(t.Context(), 0, true); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, _, _ := backfill.repository.load(t.Context())
	runOnce(t, backfill, state)
	status, _ := backfill.Status(t.Context())
	if status.State != stateComplete || status.IndexedDocuments != 1 || !status.DryRun {
		t.Fatalf("dry-run status = %+v", status)
	}
	if len(urls.rows) != 0 || len(postings.rows) != 0 || postings.calls != 0 {
		t.Fatalf(
			"dry run wrote URL rows=%d posting rows=%d calls=%d",
			len(urls.rows),
			len(postings.rows),
			postings.calls,
		)
	}
}

func TestBackfillRetriesPartialMetadataWriteAfterRestart(t *testing.T) {
	backfill, urls, postings := openBackfillFixture(t, []documentstore.Document{
		importedDocument("https://example.test/one", "200"),
	})
	postings.failCount = 1
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, _, _ := backfill.repository.load(t.Context())
	runOnce(t, backfill, state)
	status, _ := backfill.Status(t.Context())
	if status.State != stateFailed || status.ScannedDocuments != 0 || len(urls.rows) != 1 {
		t.Fatalf("interrupted status = %+v, URL rows=%d", status, len(urls.rows))
	}
	if _, err := backfill.Resume(t.Context(), 0, false); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	state, _, _ = backfill.repository.load(t.Context())
	runOnce(t, backfill, state)
	status, _ = backfill.Status(t.Context())
	if status.State != stateComplete || status.ScannedDocuments != 1 || len(urls.rows) != 1 {
		t.Fatalf("recovered status = %+v, URL rows=%d", status, len(urls.rows))
	}
	if len(postings.rows) == 0 || postings.calls < 2 {
		t.Fatalf("posting retry count=%d, posting rows=%d", postings.calls, len(postings.rows))
	}
}

func TestBackfillHonorsURLMetadataBackpressure(t *testing.T) {
	backfill, urls, postings := openBackfillFixture(t, []documentstore.Document{
		importedDocument("https://example.test/one", "200"),
	})
	urls.busyCount = 1
	if _, err := backfill.Start(t.Context(), 0, false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-backfill.wake
	state, _, _ := backfill.repository.load(t.Context())
	runOnce(t, backfill, state)
	status, _ := backfill.Status(t.Context())
	if status.State != stateComplete || len(urls.rows) != 1 || len(postings.rows) == 0 {
		t.Fatalf(
			"status = %+v, URL rows=%d posting rows=%d",
			status,
			len(urls.rows),
			len(postings.rows),
		)
	}
}

func TestBackfillArtifactMappingUsesStoredContentAndDates(t *testing.T) {
	document := importedDocument("https://example.test/page", "200")
	artifacts, ok := buildArtifacts(document)
	if !ok {
		t.Fatal("buildArtifacts rejected a valid imported document")
	}
	if artifacts.Metadata.Properties["load"] != "20240304" ||
		artifacts.Metadata.Properties["mod"] != "20240203" ||
		artifacts.Metadata.Properties["fresh"] != "20240405" {
		t.Fatalf("source dates not preserved: %v", artifacts.Metadata.Properties)
	}
	if len(artifacts.Postings) == 0 {
		t.Fatal("stored extracted text produced no terms")
	}
	for _, posting := range artifacts.Postings {
		if _, err := yagomodel.ParseRWIPosting(posting.String()); err != nil {
			t.Fatalf("posting is not wire-valid: %v", err)
		}
	}
	if _, ok := buildArtifacts(importedDocument("https://example.test/bad", "404")); ok {
		t.Fatal("failed HTTP document was accepted")
	}
	for _, rawURL := range []string{"relative/path", "file:///bad"} {
		artifacts, ok := buildArtifacts(importedDocument(rawURL, "200"))
		if !ok {
			t.Fatalf("legacy URL %q was dropped from RWI backfill", rawURL)
		}
		wantHash, err := yagomodel.HashURL(rawURL)
		if err != nil {
			t.Fatalf("hash legacy URL %q: %v", rawURL, err)
		}
		if got := artifacts.Metadata.Properties[yagomodel.URLMetaHash]; got != wantHash.String() {
			t.Fatalf("legacy URL %q hash = %q, want %q", rawURL, got, wantHash)
		}
	}
}
