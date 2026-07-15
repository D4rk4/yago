package documentstore

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type pagedDocumentEngine struct {
	mu               sync.RWMutex
	buckets          map[vault.Name]map[string][]byte
	views            atomic.Int64
	backgroundViews  atomic.Int64
	interactiveViews atomic.Int64
	activeViews      atomic.Int64
	pageReads        atomic.Int64
	pageLimit        atomic.Int64
	lastKeyReads     atomic.Int64
	pageOverride     *vault.BucketPage
	beforeUpdate     func()
	lastKeyError     error
}

func newPagedDocumentEngine() *pagedDocumentEngine {
	return &pagedDocumentEngine{buckets: map[vault.Name]map[string][]byte{}}
}

func (e *pagedDocumentEngine) Provision(name vault.Name) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *pagedDocumentEngine) Update(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("update context: %w", err)
	}
	if e.beforeUpdate != nil {
		e.beforeUpdate()
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	return fn(pagedDocumentTxn{engine: e, writable: true})
}

func (e *pagedDocumentEngine) View(
	ctx context.Context,
	fn func(vault.EngineTxn) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("view context: %w", err)
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	e.views.Add(1)
	e.activeViews.Add(1)
	defer e.activeViews.Add(-1)
	if !vault.IsBackgroundRead(ctx) {
		e.interactiveViews.Add(1)
	} else {
		e.backgroundViews.Add(1)
	}

	return fn(pagedDocumentTxn{engine: e})
}

func (e *pagedDocumentEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *pagedDocumentEngine) QuotaBytes() int64                        { return 0 }
func (e *pagedDocumentEngine) Close() error                             { return nil }

type pagedDocumentTxn struct {
	engine   *pagedDocumentEngine
	writable bool
}

func (t pagedDocumentTxn) Bucket(name vault.Name) vault.EngineBucket {
	return pagedDocumentBucket{
		engine:       t.engine,
		entries:      t.engine.buckets[name],
		pageOverride: t.engine.pageOverride,
		lastKeyError: t.engine.lastKeyError,
	}
}

func (t pagedDocumentTxn) Writable() bool { return t.writable }

type pagedDocumentBucket struct {
	engine       *pagedDocumentEngine
	entries      map[string][]byte
	pageOverride *vault.BucketPage
	lastKeyError error
}

func (b pagedDocumentBucket) Get(key vault.Key) []byte {
	return b.entries[string(key)]
}

func (b pagedDocumentBucket) Put(key vault.Key, value []byte) error {
	b.entries[string(key)] = append([]byte(nil), value...)

	return nil
}

func (b pagedDocumentBucket) Delete(key vault.Key) error {
	delete(b.entries, string(key))

	return nil
}

func (b pagedDocumentBucket) Scan(
	prefix vault.Key,
	visit func(vault.Key, []byte) (bool, error),
) error {
	for _, key := range b.orderedKeys() {
		if !strings.HasPrefix(key, string(prefix)) {
			continue
		}
		keep, err := visit(vault.Key(key), b.entries[key])
		if err != nil || !keep {
			return err
		}
	}

	return nil
}

func (b pagedDocumentBucket) ReadPageAfter(
	after vault.Key,
	limit int,
) (vault.BucketPage, error) {
	if b.engine != nil {
		b.engine.pageReads.Add(1)
		b.engine.pageLimit.Store(int64(limit))
	}
	if b.pageOverride != nil {
		return *b.pageOverride, nil
	}
	keys := b.orderedKeys()
	start := 0
	if after != nil {
		start = sort.Search(len(keys), func(index int) bool {
			return keys[index] > string(after)
		})
	}
	end := min(start+limit, len(keys))
	entries := make([]vault.BucketPageEntry, 0, end-start)
	for _, key := range keys[start:end] {
		entries = append(entries, vault.BucketPageEntry{
			Key:   vault.Key(key),
			Value: b.entries[key],
		})
	}

	return vault.BucketPage{Entries: entries, More: end < len(keys)}, nil
}

func (b pagedDocumentBucket) LastKey() (vault.Key, error) {
	if b.engine != nil {
		b.engine.lastKeyReads.Add(1)
	}
	if b.lastKeyError != nil {
		return nil, b.lastKeyError
	}
	keys := b.orderedKeys()
	if len(keys) == 0 {
		return nil, nil
	}

	return vault.Key(keys[len(keys)-1]), nil
}

func (b pagedDocumentBucket) orderedKeys() []string {
	keys := make([]string, 0, len(b.entries))
	for key := range b.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

func openPagedDocuments(
	t *testing.T,
) (DocumentDirectory, DocumentReceiver, *pagedDocumentEngine) {
	t.Helper()
	engine := newPagedDocumentEngine()
	_, directory, receiver := openPagedDocumentsOnEngine(t, engine)
	engine.views.Store(0)
	engine.backgroundViews.Store(0)
	engine.interactiveViews.Store(0)
	engine.pageReads.Store(0)
	engine.pageLimit.Store(0)
	engine.lastKeyReads.Store(0)

	return directory, receiver, engine
}

func openPagedDocumentsOnEngine(
	t *testing.T,
	engine *pagedDocumentEngine,
) (*vault.Vault, DocumentDirectory, DocumentReceiver) {
	t.Helper()
	vaulted, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vaulted.Close() })
	directory, receiver, err := Open(vaulted)
	if err != nil {
		t.Fatal(err)
	}

	return vaulted, directory, receiver
}

func receivePagedDocuments(
	t *testing.T,
	receiver DocumentReceiver,
	keys ...string,
) {
	t.Helper()
	documents := make([]Document, 0, len(keys))
	for _, key := range keys {
		documents = append(documents, Document{
			NormalizedURL: key,
			Title:         "title " + key,
		})
	}
	if _, err := receiver.Receive(t.Context(), documents); err != nil {
		t.Fatal(err)
	}
}

func scanStoredDocumentPagesForTest(
	directory DocumentDirectory,
	ctx context.Context,
	pageSize int,
	visit func(Document) (bool, error),
) error {
	documents := directory.(documentVault)
	release, err := documents.enterStoredDocumentScan(ctx)
	if err != nil {
		return err
	}
	defer release()

	return documents.scanStoredDocumentPages(ctx, pageSize, visit)
}

func TestStoredDocumentPageScannerTraversesOrderedShortViews(t *testing.T) {
	directory, receiver, engine := openPagedDocuments(t)
	receivePagedDocuments(t, receiver, "d", "b", "e", "a", "c")
	var visited []string
	err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		2,
		func(document Document) (bool, error) {
			if engine.activeViews.Load() != 0 {
				t.Fatal("visitor ran inside a storage View")
			}
			visited = append(visited, document.NormalizedURL)

			return true, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(visited, ","), "d,b,e,a,c"; got != want {
		t.Fatalf("visited = %q, want %q", got, want)
	}
	if engine.backgroundViews.Load() != 7 || engine.interactiveViews.Load() != 1 ||
		engine.pageReads.Load() != 3 {
		t.Fatalf(
			"background/interactive/pages = %d/%d/%d",
			engine.backgroundViews.Load(),
			engine.interactiveViews.Load(),
			engine.pageReads.Load(),
		)
	}
}

func TestStoredDocumentPageScannerFixesRawPageLimit(t *testing.T) {
	directory, receiver, engine := openPagedDocuments(t)
	urls := make([]string, 17)
	for index := range urls {
		urls[index] = fmt.Sprintf("https://page-limit.example/%02d", index)
	}
	receivePagedDocuments(t, receiver, urls...)
	engine.pageReads.Store(0)
	engine.pageLimit.Store(0)
	visits := 0
	err := directory.(StoredDocumentPageScanner).ScanStoredDocumentPages(
		t.Context(),
		func(Document) (bool, error) {
			visits++

			return true, nil
		},
	)
	if err != nil || visits != len(urls) ||
		engine.pageLimit.Load() != storedDocumentPageSize || engine.pageReads.Load() != 2 {
		t.Fatalf(
			"fixed page scan = %v, visits %d, limit %d, pages %d",
			err,
			visits,
			engine.pageLimit.Load(),
			engine.pageReads.Load(),
		)
	}
}

func TestStoredDocumentPageScannerStopsAndReturnsVisitorError(t *testing.T) {
	directory, receiver, engine := openPagedDocuments(t)
	receivePagedDocuments(t, receiver, "a", "b", "c")
	visits := 0
	if err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		2,
		func(Document) (bool, error) {
			visits++

			return false, nil
		},
	); err != nil || visits != 1 ||
		engine.backgroundViews.Load() != 3 {
		t.Fatalf(
			"stop error/visits/views = %v/%d/%d",
			err,
			visits,
			engine.backgroundViews.Load(),
		)
	}
	sentinel := errors.New("visitor failed")
	err := scanStoredDocumentPagesForTest(directory, t.Context(), 2, func(Document) (bool, error) {
		return false, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("visitor error = %v, want %v", err, sentinel)
	}
}

func TestStoredDocumentPageScannerHonorsCancellation(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	receivePagedDocuments(t, receiver, "a", "b", "c")
	ctx, cancel := context.WithCancel(t.Context())
	visits := 0
	err := scanStoredDocumentPagesForTest(
		directory,
		ctx,
		2,
		func(Document) (bool, error) {
			visits++
			cancel()

			return true, nil
		},
	)
	if !errors.Is(err, context.Canceled) || visits != 1 {
		t.Fatalf("cancel error/visits = %v/%d", err, visits)
	}
}

func TestStoredDocumentPageScannerConcurrentInsertBoundary(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	receivePagedDocuments(t, receiver, "a", "b", "c")
	var visited []string
	err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		1,
		func(document Document) (bool, error) {
			visited = append(visited, document.NormalizedURL)
			if len(visited) > 10 {
				return false, errors.New("scan exceeded its initial document boundary")
			}
			_, err := receiver.Receive(t.Context(), []Document{{
				NormalizedURL: fmt.Sprintf("z-%03d", len(visited)),
			}})
			if err != nil {
				return false, fmt.Errorf("receive concurrent document: %w", err)
			}

			return true, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(visited, ","), "a,b,c"; got != want {
		t.Fatalf("visited = %q, want %q", got, want)
	}
}

func TestStoredDocumentPageScannerAdvancesPastSustainedInteriorInserts(t *testing.T) {
	directory, receiver, engine := openPagedDocuments(t)
	receivePagedDocuments(t, receiver, "a", "b", "c")
	var visited []string
	err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		1,
		func(document Document) (bool, error) {
			visited = append(visited, document.NormalizedURL)
			if document.NormalizedURL == "c" {
				return true, nil
			}
			inserted := make([]Document, 0, 256)
			for sequence := range 256 {
				inserted = append(inserted, Document{
					NormalizedURL: fmt.Sprintf(
						"%s-%03d",
						document.NormalizedURL,
						sequence,
					),
				})
			}
			if _, err := receiver.Receive(t.Context(), inserted); err != nil {
				return false, fmt.Errorf("receive interior documents: %w", err)
			}

			return true, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(visited, ","), "a,b,c"; got != want {
		t.Fatalf("visited = %q, want %q", got, want)
	}
	if engine.backgroundViews.Load() != 7 {
		t.Fatalf("storage views = %d, want 7", engine.backgroundViews.Load())
	}
}

func TestStoredDocumentPageScannerStopsPastDeletedLastKey(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	receivePagedDocuments(t, receiver, "a", "b", "c")
	var visited []string
	err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		1,
		func(document Document) (bool, error) {
			visited = append(visited, document.NormalizedURL)
			if document.NormalizedURL == "a" {
				if _, err := directory.(DocumentEvictor).Delete(t.Context(), "c"); err != nil {
					return false, fmt.Errorf("delete initial last document: %w", err)
				}
				if _, err := receiver.Receive(t.Context(), []Document{{
					NormalizedURL: "z",
				}}); err != nil {
					return false, fmt.Errorf("receive post-boundary document: %w", err)
				}
			}

			return true, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(visited, ","), "a,b"; got != want {
		t.Fatalf("visited = %q, want %q", got, want)
	}
}

func TestStoredDocumentPageScannerExcludesInsertedEarlierKey(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	receivePagedDocuments(t, receiver, "a", "b", "c")
	var visited []string
	err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		1,
		func(document Document) (bool, error) {
			visited = append(visited, document.NormalizedURL)
			if document.NormalizedURL == "a" {
				if _, err := receiver.Receive(t.Context(), []Document{{
					NormalizedURL: "aa",
					Title:         "inserted",
				}}); err != nil {
					return false, fmt.Errorf("receive inserted document: %w", err)
				}
			}

			return true, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(visited, ","), "a,b,c"; got != want {
		t.Fatalf("visited = %q, want %q", got, want)
	}
	if inserted, found, err := directory.Document(t.Context(), "aa"); err != nil ||
		!found || inserted.Title != "inserted" {
		t.Fatalf("inserted document = %#v, %t, %v", inserted, found, err)
	}
}

func TestStoredDocumentPageScannerPreservesMembershipAcrossUpdates(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	receivePagedDocuments(t, receiver, "a", "b", "c")
	var visited []string
	err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		1,
		func(document Document) (bool, error) {
			visited = append(visited, document.NormalizedURL+":"+document.Title)
			if document.NormalizedURL != "a" {
				return true, nil
			}
			if _, err := receiver.Receive(t.Context(), []Document{
				{NormalizedURL: "aa", Title: "inserted"},
				{NormalizedURL: "b", Title: "updated b"},
			}); err != nil {
				return false, fmt.Errorf("receive scan updates: %w", err)
			}
			if _, err := receiver.Receive(t.Context(), []Document{{
				NormalizedURL: "aa",
				Title:         "updated inserted",
			}}); err != nil {
				return false, fmt.Errorf("update inserted document: %w", err)
			}

			return true, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(visited, ","), "a:title a,b:updated b,c:title c"; got != want {
		t.Fatalf("visited = %q, want %q", got, want)
	}
}

func TestStoredDocumentPageScannerWaitsForAdmittedWriter(t *testing.T) {
	previousParallelism := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousParallelism) })
	directory, receiver, engine := openPagedDocuments(t)
	receivePagedDocuments(t, receiver, "a")
	updateEntered := make(chan struct{})
	releaseUpdate := make(chan struct{})
	var blockFirstUpdate sync.Once
	engine.beforeUpdate = func() {
		blockFirstUpdate.Do(func() {
			close(updateEntered)
			<-releaseUpdate
		})
	}
	writeDone := make(chan error, 1)
	go func() {
		_, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: "b"}})
		writeDone <- err
	}()
	<-updateEntered

	scanStarted := make(chan struct{})
	visited := make(chan []string, 1)
	scanDone := make(chan error, 1)
	go func() {
		close(scanStarted)
		var found []string
		err := scanStoredDocumentPagesForTest(
			directory,
			t.Context(),
			1,
			func(document Document) (bool, error) {
				found = append(found, document.NormalizedURL)

				return true, nil
			},
		)
		visited <- found
		scanDone <- err
	}()
	<-scanStarted
	runtime.Gosched()
	if engine.backgroundViews.Load() != 0 {
		t.Fatal("initial scan view crossed an admitted writer")
	}
	close(releaseUpdate)
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-scanDone; err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(<-visited, ","), "a,b"; got != want {
		t.Fatalf("visited = %q, want %q", got, want)
	}
}

func TestStoredDocumentPageScannerEmptyBoundary(t *testing.T) {
	directory, _, engine := openPagedDocuments(t)
	visits := 0
	err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		2,
		func(Document) (bool, error) {
			visits++

			return true, nil
		},
	)
	if err != nil || visits != 0 || engine.backgroundViews.Load() != 1 {
		t.Fatalf(
			"empty scan error/visits/views = %v/%d/%d",
			err,
			visits,
			engine.backgroundViews.Load(),
		)
	}
}

func TestStoredDocumentPageScannerIgnoresLengthDrift(t *testing.T) {
	directory, _, engine := openPagedDocuments(t)
	engine.mu.Lock()
	engine.buckets[vault.Name("__lengths__")][string(bucketName)] = []byte("bad")
	engine.mu.Unlock()
	visits := 0
	err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		1,
		func(Document) (bool, error) {
			visits++

			return true, nil
		},
	)
	if err != nil || visits != 0 {
		t.Fatalf("length drift scan error/visits = %v/%d", err, visits)
	}
}

func TestStoredDocumentPageScannerReportsLastKeyError(t *testing.T) {
	directory, receiver, engine := openPagedDocuments(t)
	receivePagedDocuments(t, receiver, "a")
	sentinel := errors.New("last key failed")
	engine.lastKeyError = sentinel
	err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		1,
		func(Document) (bool, error) {
			t.Fatal("last-key error scan visited a document")

			return false, nil
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("last-key error = %v, want %v", err, sentinel)
	}
}

func TestStoredDocumentPageScannerReportsLaterPageError(t *testing.T) {
	directory, receiver, engine := openPagedDocuments(t)
	receivePagedDocuments(t, receiver, "a", "b")
	visits := 0
	err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		1,
		func(Document) (bool, error) {
			visits++
			engine.pageOverride = &vault.BucketPage{More: true}

			return true, nil
		},
	)
	if err == nil || visits != 1 {
		t.Fatalf("later page error/visits = %v/%d", err, visits)
	}
}

func TestStoredDocumentPageScannerReportsReadAndDecodeErrors(t *testing.T) {
	directory, _, _ := openPagedDocuments(t)
	err := scanStoredDocumentPagesForTest(
		directory,
		t.Context(),
		0,
		func(Document) (bool, error) { return true, nil },
	)
	if err == nil {
		t.Fatal("zero page size accepted")
	}
	invalidDirectory, invalidReceiver, invalidEngine := openPagedDocuments(t)
	receivePagedDocuments(t, invalidReceiver, "bad")
	invalidEngine.mu.Lock()
	orderedKeys := pagedDocumentBucket{
		entries: invalidEngine.buckets[orderedDocumentBucketName],
	}.orderedKeys()
	invalidEngine.buckets[orderedDocumentBucketName][orderedKeys[0]] = []byte("{")
	invalidEngine.mu.Unlock()
	visits := 0
	err = scanStoredDocumentPagesForTest(
		invalidDirectory,
		t.Context(),
		1,
		func(Document) (bool, error) {
			visits++

			return true, nil
		},
	)
	if err != nil || visits != 0 {
		t.Fatalf("invalid stored page = %v, %d visits", err, visits)
	}
	malformedDirectory, malformedReceiver, malformedEngine := openPagedDocuments(t)
	receivePagedDocuments(t, malformedReceiver, "bad")
	malformedEngine.pageOverride = &vault.BucketPage{More: true}
	err = scanStoredDocumentPagesForTest(
		malformedDirectory,
		t.Context(),
		1,
		func(Document) (bool, error) { return true, nil },
	)
	if err == nil {
		t.Fatal("non-advancing stored page accepted")
	}
	documents, err := decodeStoredDocumentRawPage(
		t.Context(),
		[]vault.BucketPageEntry{{Key: vault.Key("a"), Value: []byte("{")}},
	)
	if err != nil || len(documents) != 0 {
		t.Fatalf("invalid stored document = %#v, %v", documents, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := decodeStoredDocumentRawPage(
		ctx,
		[]vault.BucketPageEntry{{Key: vault.Key("a"), Value: []byte("{}")}},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("decode cancellation = %v", err)
	}
}

func TestStoredDocumentPageScannerWaitHonorsCancellation(t *testing.T) {
	directory, _, _ := openPagedDocuments(t)
	documents := directory.(documentVault)
	release, err := documents.enterStoredDocumentScan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = scanStoredDocumentPagesForTest(documents, ctx, 1, func(Document) (bool, error) {
		return true, fmt.Errorf("visitor should not run")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scan admission cancellation = %v", err)
	}
}
