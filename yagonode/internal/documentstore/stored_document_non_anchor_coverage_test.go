package documentstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type documentStorageFaultEngine struct {
	base          *pagedDocumentEngine
	viewError     error
	updateError   error
	putErrors     map[vault.Name]error
	deleteErrors  map[vault.Name]error
	pageErrors    map[vault.Name]error
	lastKeyErrors map[vault.Name]error
}

type documentStorageObservedContext struct {
	context.Context
	observed chan struct{}
}

func (c documentStorageObservedContext) Done() <-chan struct{} {
	select {
	case c.observed <- struct{}{}:
	default:
	}

	return c.Context.Done()
}

func newDocumentStorageFaultEngine() *documentStorageFaultEngine {
	return &documentStorageFaultEngine{
		base:          newPagedDocumentEngine(),
		putErrors:     make(map[vault.Name]error),
		deleteErrors:  make(map[vault.Name]error),
		pageErrors:    make(map[vault.Name]error),
		lastKeyErrors: make(map[vault.Name]error),
	}
}

func (e *documentStorageFaultEngine) Provision(name vault.Name) error {
	return e.base.Provision(name)
}

func (e *documentStorageFaultEngine) Update(
	ctx context.Context,
	visit func(vault.EngineTxn) error,
) error {
	if e.updateError != nil {
		return e.updateError
	}

	return e.base.Update(ctx, func(tx vault.EngineTxn) error {
		return visit(documentStorageFaultTxn{EngineTxn: tx, engine: e})
	})
}

func (e *documentStorageFaultEngine) View(
	ctx context.Context,
	visit func(vault.EngineTxn) error,
) error {
	if e.viewError != nil {
		return e.viewError
	}

	return e.base.View(ctx, func(tx vault.EngineTxn) error {
		return visit(documentStorageFaultTxn{EngineTxn: tx, engine: e})
	})
}

func (e *documentStorageFaultEngine) UsedBytes(ctx context.Context) (int64, error) {
	return e.base.UsedBytes(ctx)
}

func (e *documentStorageFaultEngine) QuotaBytes() int64 {
	return e.base.QuotaBytes()
}

func (e *documentStorageFaultEngine) Close() error {
	return e.base.Close()
}

func (e *documentStorageFaultEngine) putRaw(
	bucket vault.Name,
	key vault.Key,
	value []byte,
) {
	e.base.mu.Lock()
	defer e.base.mu.Unlock()
	e.base.buckets[bucket][string(key)] = append([]byte(nil), value...)
}

type documentStorageFaultTxn struct {
	vault.EngineTxn
	engine *documentStorageFaultEngine
}

func (t documentStorageFaultTxn) Bucket(name vault.Name) vault.EngineBucket {
	return documentStorageFaultBucket{
		EngineBucket: t.EngineTxn.Bucket(name),
		engine:       t.engine,
		name:         name,
	}
}

type documentStorageFaultBucket struct {
	vault.EngineBucket
	engine *documentStorageFaultEngine
	name   vault.Name
}

func (b documentStorageFaultBucket) Put(key vault.Key, value []byte) error {
	if err := b.engine.putErrors[b.name]; err != nil {
		return err
	}
	if err := b.EngineBucket.Put(key, value); err != nil {
		return fmt.Errorf("put fault document bucket: %w", err)
	}

	return nil
}

func (b documentStorageFaultBucket) Delete(key vault.Key) error {
	if err := b.engine.deleteErrors[b.name]; err != nil {
		return err
	}

	if err := b.EngineBucket.Delete(key); err != nil {
		return fmt.Errorf("delete fault document bucket: %w", err)
	}

	return nil
}

func (b documentStorageFaultBucket) ReadPageAfter(
	after vault.Key,
	limit int,
) (vault.BucketPage, error) {
	if err := b.engine.pageErrors[b.name]; err != nil {
		return vault.BucketPage{}, err
	}

	page, err := b.EngineBucket.(interface {
		ReadPageAfter(vault.Key, int) (vault.BucketPage, error)
	}).ReadPageAfter(after, limit)
	if err != nil {
		return vault.BucketPage{}, fmt.Errorf("read fault document page: %w", err)
	}

	return page, nil
}

func (b documentStorageFaultBucket) LastKey() (vault.Key, error) {
	if err := b.engine.lastKeyErrors[b.name]; err != nil {
		return nil, err
	}

	key, err := b.EngineBucket.(interface {
		LastKey() (vault.Key, error)
	}).LastKey()
	if err != nil {
		return nil, fmt.Errorf("read fault document last key: %w", err)
	}

	return key, nil
}

func openDocumentStorageFaultVault(
	t *testing.T,
) (*vault.Vault, documentVault, *documentStorageFaultEngine) {
	t.Helper()
	engine := newDocumentStorageFaultEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	_, receiver, err := Open(storage)
	if err != nil {
		t.Fatal(err)
	}

	return storage, receiver.(documentVault), engine
}

func registeredDocumentStorageFaultVault(
	t *testing.T,
) (*vault.Vault, *vault.Keyspace[uint64], *documentStorageFaultEngine) {
	t.Helper()
	engine := newDocumentStorageFaultEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	_, _, _, admissions, err := registerDocumentCollections(storage)
	if err != nil {
		t.Fatal(err)
	}

	return storage, admissions, engine
}

func TestRegisterDocumentCollectionsReportsEveryLaterDuplicate(t *testing.T) {
	tests := []struct {
		name   string
		bucket vault.Name
		codec  vault.Codec[uint64]
	}{
		{name: "ordered", bucket: orderedDocumentBucketName},
		{name: "locations", bucket: documentLocationBucketName, codec: documentLocationCodec{}},
		{name: "admissions", bucket: documentAdmissionBucketName, codec: documentLocationCodec{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := newDocumentStorageFaultEngine()
			storage, err := vault.New(engine)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = storage.Close() }()
			if test.bucket == orderedDocumentBucketName {
				_, err := vault.RegisterKeyspace(
					storage,
					test.bucket,
					documentCodec{},
				)
				if err != nil {
					t.Fatal(err)
				}
			} else if _, err := vault.RegisterKeyspace(storage, test.bucket, test.codec); err != nil {
				t.Fatal(err)
			}
			if _, _, _, _, err := registerDocumentCollections(storage); err == nil {
				t.Fatal("duplicate document collection was accepted")
			}
		})
	}
}

func TestOpenStoredDocumentAdmissionKeysReportsStorageFailures(t *testing.T) {
	t.Run("high water decode", func(t *testing.T) {
		storage, admissions, engine := registeredDocumentStorageFaultVault(t)
		engine.putRaw(documentAdmissionBucketName, documentAdmissionHighWaterKey, []byte{1})
		if _, err := openStoredDocumentAdmissionKeys(storage, admissions); err == nil {
			t.Fatal("malformed high water was accepted")
		}
	})
	t.Run("last key read", func(t *testing.T) {
		storage, admissions, engine := registeredDocumentStorageFaultVault(t)
		engine.lastKeyErrors[orderedDocumentBucketName] = errors.New("last key")
		if _, err := openStoredDocumentAdmissionKeys(storage, admissions); err == nil {
			t.Fatal("last-key failure was ignored")
		}
	})
	t.Run("last key decode", func(t *testing.T) {
		storage, admissions, engine := registeredDocumentStorageFaultVault(t)
		engine.putRaw(orderedDocumentBucketName, vault.Key("short"), []byte("{}"))
		if _, err := openStoredDocumentAdmissionKeys(storage, admissions); err == nil {
			t.Fatal("malformed physical key was accepted")
		}
	})
	t.Run("view", func(t *testing.T) {
		storage, admissions, engine := registeredDocumentStorageFaultVault(t)
		engine.viewError = errors.New("view")
		if _, err := openStoredDocumentAdmissionKeys(storage, admissions); err == nil {
			t.Fatal("admission view failure was ignored")
		}
	})
	t.Run("reread", func(t *testing.T) {
		storage, admissions, engine := registeredDocumentStorageFaultVault(t)
		key, err := orderedDocumentKey(2, "https://admission.example/reread")
		if err != nil {
			t.Fatal(err)
		}
		engine.putRaw(orderedDocumentBucketName, key, []byte("{}"))
		engine.base.beforeUpdate = func() {
			engine.putRaw(documentAdmissionBucketName, documentAdmissionHighWaterKey, []byte{1})
		}
		if _, err := openStoredDocumentAdmissionKeys(storage, admissions); err == nil {
			t.Fatal("admission reread failure was ignored")
		}
	})
	t.Run("persist", func(t *testing.T) {
		storage, admissions, engine := registeredDocumentStorageFaultVault(t)
		key, err := orderedDocumentKey(2, "https://admission.example/persist")
		if err != nil {
			t.Fatal(err)
		}
		engine.putRaw(orderedDocumentBucketName, key, []byte("{}"))
		engine.putErrors[documentAdmissionBucketName] = errors.New("put")
		if _, err := openStoredDocumentAdmissionKeys(storage, admissions); err == nil {
			t.Fatal("admission persistence failure was ignored")
		}
	})
}

func TestStoredDocumentAdmissionKeysReportsDurableReadFailure(t *testing.T) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	engine.putRaw(documentAdmissionBucketName, documentAdmissionHighWaterKey, []byte{1})
	if _, err := documents.admissionKeys.issue(t.Context(), 1); err == nil {
		t.Fatal("malformed durable admission was accepted")
	}
}

func TestReceiveReportsWriteAndURLBoundaryCancellation(t *testing.T) {
	_, documents, _ := openDocumentStorageFaultVault(t)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := documents.Receive(cancelled, []Document{{
		NormalizedURL: "https://cancel.example/write",
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("write-boundary cancellation = %v", err)
	}
	url := "https://cancel.example/url"
	release, err := documents.urlBoundaries.lockWrites(t.Context(), []string{url})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancelURL := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := documents.Receive(ctx, []Document{{NormalizedURL: url}})
		result <- err
	}()
	index := documents.urlBoundaries.indices([]string{url})[0]
	deadline := time.Now().Add(time.Second)
	for {
		documents.urlBoundaries.entries[index].mutex.Lock()
		waiting := documents.urlBoundaries.entries[index].waitingWriters
		documents.urlBoundaries.entries[index].mutex.Unlock()
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("receive did not wait on the URL boundary")
		}
		time.Sleep(time.Millisecond)
	}
	cancelURL()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("URL-boundary cancellation = %v", err)
	}
}

func TestStoreOneReportsContextAndMissingAdmission(t *testing.T) {
	storage, documents, _ := openDocumentStorageFaultVault(t)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		attempt := newStoredDocumentWriteAttempt(storedDocumentWritePlan{})

		return documents.storeOne(cancelled, tx, Document{}, &attempt)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("store context error = %v", err)
	}
	err = storage.Update(t.Context(), func(tx *vault.Txn) error {
		attempt := newStoredDocumentWriteAttempt(storedDocumentWritePlan{
			admissions:         make(map[string]uint64),
			existing:           make(map[string]stagedStoredDocument),
			previousAdmissions: make(map[string]uint64),
		})

		return documents.storeOne(
			t.Context(),
			tx,
			Document{NormalizedURL: "https://missing.example/admission"},
			&attempt,
		)
	})
	if err == nil {
		t.Fatal("unplanned document admission was accepted")
	}
}

func TestPublishDocumentLocationReportsAllConflicts(t *testing.T) {
	storage, documents, engine := openDocumentStorageFaultVault(t)
	url := "https://publication.example/document"
	engine.putRaw(documentLocationBucketName, vault.Key(url), []byte{1})
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return documents.publishDocumentLocation(
			tx,
			url,
			storedDocumentLocationPublication{admission: 2},
		)
	}); err == nil {
		t.Fatal("malformed published location was accepted")
	}
	encoded, err := encodeOrderedDocumentAdmission(2)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(documentLocationBucketName, vault.Key(url), encoded)
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return documents.publishDocumentLocation(
			tx,
			url,
			storedDocumentLocationPublication{admission: 2},
		)
	}); err != nil {
		t.Fatalf("idempotent publication = %v", err)
	}
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return documents.publishDocumentLocation(
			tx,
			url,
			storedDocumentLocationPublication{admission: 3},
		)
	}); err == nil {
		t.Fatal("unexpected first publication location was accepted")
	}
}

func TestCanonicalDocumentsReportsLoopAndStoredEvidenceFailures(t *testing.T) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	delayed := &errAfterContext{
		Context:   context.Background(),
		remaining: 3,
		err:       context.Canceled,
	}
	if _, err := documents.CanonicalDocuments(delayed, []Document{{
		NormalizedURL: "https://canonical.example/cancel",
	}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canonical loop cancellation = %v", err)
	}
	url := "https://canonical.example/malformed-location"
	engine.putRaw(documentLocationBucketName, vault.Key(url), []byte{1})
	if _, err := documents.CanonicalDocuments(t.Context(), []Document{{
		NormalizedURL: url,
	}}); err == nil {
		t.Fatal("malformed canonical location was accepted")
	}
}

func TestDocumentPresenceRejectsUnresolvableLocatedURL(t *testing.T) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	encoded, err := encodeOrderedDocumentAdmission(1)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(documentLocationBucketName, vault.Key(""), encoded)
	if _, err := documents.DocumentExists(t.Context(), ""); err == nil {
		t.Fatal("empty located URL presence succeeded")
	}
}

func TestDeleteReportsURLBoundaryCancellation(t *testing.T) {
	_, documents, _ := openDocumentStorageFaultVault(t)
	url := "https://delete.example/cancel"
	release, err := documents.urlBoundaries.lockWrites(t.Context(), []string{url})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := documents.Delete(ctx, url)
		result <- err
	}()
	index := documents.urlBoundaries.indices([]string{url})[0]
	deadline := time.Now().Add(time.Second)
	for {
		documents.urlBoundaries.entries[index].mutex.Lock()
		waiting := documents.urlBoundaries.entries[index].waitingWriters
		documents.urlBoundaries.entries[index].mutex.Unlock()
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("delete did not wait on the URL boundary")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("delete URL-boundary cancellation = %v", err)
	}
}

func TestDeleteStoredDocumentReportsStorageStateFailures(t *testing.T) {
	t.Run("location", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		url := "https://delete.example/location"
		engine.putRaw(documentLocationBucketName, vault.Key(url), []byte{1})
		if _, err := documents.deleteStoredDocument(t.Context(), url); err == nil {
			t.Fatal("malformed delete location was accepted")
		}
	})
	t.Run("view", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		engine.viewError = errors.New("view")
		if _, _, _, err := documents.locateStoredDocumentDeletion(
			t.Context(),
			"https://delete.example/view",
		); err == nil {
			t.Fatal("delete location view failure was ignored")
		}
	})
	t.Run("legacy delete", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		url := "https://delete.example/legacy"
		raw, err := (documentCodec{}).Encode(Document{NormalizedURL: url})
		if err != nil {
			t.Fatal(err)
		}
		engine.putRaw(bucketName, vault.Key(url), raw)
		engine.deleteErrors[bucketName] = errors.New("delete")
		if _, err := documents.deleteLegacyStoredDocument(t.Context(), url); err == nil {
			t.Fatal("legacy delete failure was ignored")
		}
	})
}

func TestDeleteOrderedDocumentRowsReportsEveryGuard(t *testing.T) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	if err := documents.deleteOrderedDocumentRows(t.Context(), "", 1, false); err == nil {
		t.Fatal("empty ordered delete URL was accepted")
	}
	url := "https://delete.example/ordered"
	engine.putRaw(documentLocationBucketName, vault.Key(url), []byte{1})
	if err := documents.deleteOrderedDocumentRows(t.Context(), url, 1, false); err == nil {
		t.Fatal("malformed ordered delete location was accepted")
	}
	encoded, err := encodeOrderedDocumentAdmission(2)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(documentLocationBucketName, vault.Key(url), encoded)
	if err := documents.deleteOrderedDocumentRows(t.Context(), url, 1, false); err == nil {
		t.Fatal("changed ordered delete location was accepted")
	}
}

func TestDeleteOrderedDocumentLocationReportsEveryGuard(t *testing.T) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	url := "https://delete.example/hidden"
	engine.putRaw(documentLocationBucketName, vault.Key(url), []byte{1})
	if err := documents.deleteOrderedDocumentLocation(t.Context(), url, 1); err == nil {
		t.Fatal("malformed hidden location was accepted")
	}
	engine.base.mu.Lock()
	delete(engine.base.buckets[documentLocationBucketName], url)
	engine.base.mu.Unlock()
	if err := documents.deleteOrderedDocumentLocation(t.Context(), url, 1); err != nil {
		t.Fatalf("missing hidden location = %v", err)
	}
	encoded, err := encodeOrderedDocumentAdmission(2)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(documentLocationBucketName, vault.Key(url), encoded)
	if err := documents.deleteOrderedDocumentLocation(t.Context(), url, 1); err == nil {
		t.Fatal("changed hidden location was accepted")
	}
}

func TestFailedDocumentPublicationReportsCleanupFailures(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		admission uint64
		prepare   func(*documentStorageFaultEngine, string)
	}{
		{
			name:      "location",
			url:       "https://cleanup.example/location",
			admission: 1,
			prepare: func(engine *documentStorageFaultEngine, url string) {
				engine.putRaw(documentLocationBucketName, vault.Key(url), []byte{1})
			},
		},
		{name: "key", admission: 1},
		{
			name:      "delete",
			url:       "https://cleanup.example/delete",
			admission: 1,
			prepare: func(engine *documentStorageFaultEngine, _ string) {
				engine.deleteErrors[orderedDocumentBucketName] = errors.New("delete")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, documents, engine := openDocumentStorageFaultVault(t)
			if test.prepare != nil {
				test.prepare(engine, test.url)
			}
			if test.name == "delete" {
				key, err := orderedDocumentKey(test.admission, test.url)
				if err != nil {
					t.Fatal(err)
				}
				engine.putRaw(orderedDocumentBucketName, key, []byte("{}"))
			}
			publicationError := errors.New("publication")
			err := documents.recoverFailedDocumentPublication(
				t.Context(),
				map[string]storedDocumentLocationPublication{
					test.url: {admission: test.admission},
				},
				publicationError,
			)
			if !errors.Is(err, publicationError) || !strings.Contains(err.Error(), "clean failed") {
				t.Fatalf("cleanup failure = %v", err)
			}
		})
	}
}

func TestStoredDocumentLocationReportsMalformedAndMismatchedValues(t *testing.T) {
	storage, documents, engine := openDocumentStorageFaultVault(t)
	url := "https://location.example/malformed"
	engine.putRaw(documentLocationBucketName, vault.Key(url), []byte{1})
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		_, _, err := documents.locateStoredDocument(tx, url)

		return err
	}); err == nil {
		t.Fatal("malformed location was accepted")
	}
	emptyAdmission, err := encodeOrderedDocumentAdmission(1)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(documentLocationBucketName, vault.Key(""), emptyAdmission)
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		_, _, _, err := documents.readStoredDocument(tx, "")

		return err
	}); err == nil {
		t.Fatal("empty ordered location was accepted")
	}
	wrongURL := "https://location.example/wrong"
	encoded, err := encodeOrderedDocumentAdmission(2)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(documentLocationBucketName, vault.Key(wrongURL), encoded)
	key, err := orderedDocumentKey(2, wrongURL)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := (documentCodec{}).Encode(Document{NormalizedURL: "https://other.example/"})
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(orderedDocumentBucketName, key, raw)
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		_, _, found, err := documents.readStoredDocument(tx, wrongURL)
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("mismatched document was visible")
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPutStoredDocumentReportsLegacyAndKeyFailures(t *testing.T) {
	storage, documents, engine := openDocumentStorageFaultVault(t)
	engine.putErrors[bucketName] = errors.New("legacy put")
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return documents.putStoredDocument(
			tx,
			storedDocumentLocation{},
			Document{NormalizedURL: "https://put.example/legacy"},
		)
	}); err == nil {
		t.Fatal("legacy put failure was ignored")
	}
	delete(engine.putErrors, bucketName)
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return documents.putStoredDocument(
			tx,
			storedDocumentLocation{admission: 1},
			Document{},
		)
	}); err == nil {
		t.Fatal("empty ordered document URL was accepted")
	}
}

func TestStoredDocumentScannerReportsBoundaryAndPageFailures(t *testing.T) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := documents.captureStoredDocumentPartitionBoundaries(cancelled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scan boundary cancellation = %v", err)
	}
	engine.pageErrors[orderedDocumentBucketName] = errors.New("page")
	boundary := storedDocumentPartitionBoundary{
		partition: orderedDocumentPartition,
		bucket:    orderedDocumentBucketName,
		lastKey:   vault.Key("z"),
	}
	if err := documents.scanStoredDocumentPartition(
		t.Context(),
		boundary,
		1,
		func(Document) (bool, error) { return true, nil },
	); err == nil {
		t.Fatal("page failure was ignored")
	}
}

func TestStoredDocumentScannerReportsAuthorityFailures(t *testing.T) {
	storage, documents, engine := openDocumentStorageFaultVault(t)
	url := "https://scanner.example/location"
	engine.putRaw(documentLocationBucketName, vault.Key(url), []byte{1})
	raw, err := (documentCodec{}).Encode(Document{NormalizedURL: url})
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(bucketName, vault.Key(url), raw)
	legacyEntry := vault.BucketPageEntry{Key: vault.Key(url), Value: raw}
	if _, err := documents.authoritativeStoredDocumentRawPage(
		t.Context(),
		legacyDocumentPartition,
		[]vault.BucketPageEntry{legacyEntry},
	); err == nil {
		t.Fatal("legacy authority failure was ignored")
	}
	if _, err := documents.authoritativeStoredDocumentRawPage(
		&errAfterContext{
			Context:   context.Background(),
			remaining: 2,
			err:       context.Canceled,
		},
		legacyDocumentPartition,
		[]vault.BucketPageEntry{legacyEntry},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("authority cancellation = %v", err)
	}
	if err := documents.scanStoredDocumentPartition(
		t.Context(),
		storedDocumentPartitionBoundary{
			partition: legacyDocumentPartition,
			bucket:    bucketName,
			lastKey:   vault.Key(url),
		},
		1,
		func(Document) (bool, error) { return true, nil },
	); err == nil {
		t.Fatal("partition authority failure was ignored")
	}
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		visible, err := documents.storedDocumentPageEntryAuthority(
			tx,
			orderedDocumentPartition,
			vault.BucketPageEntry{Key: vault.Key("bad")},
		)
		if err != nil {
			return fmt.Errorf("malformed ordered authority: %w", err)
		}
		if visible {
			return errors.New("malformed ordered authority was visible")
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	orderedURL := "https://scanner.example/ordered-location"
	orderedKey, err := orderedDocumentKey(1, orderedURL)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(documentLocationBucketName, vault.Key(orderedURL), []byte{1})
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		_, err := documents.storedDocumentPageEntryAuthority(
			tx,
			orderedDocumentPartition,
			vault.BucketPageEntry{Key: orderedKey},
		)

		return err
	}); err == nil {
		t.Fatal("ordered authority location failure was ignored")
	}
}

func TestStoredDocumentScannerReportsVisiblePageFailures(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := visibleStoredDocumentPage(
		cancelled,
		legacyDocumentPartition,
		[]storedDocumentPageEntry{{key: vault.Key("document")}},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("visible page cancellation = %v", err)
	}
	documents, err := visibleStoredDocumentPage(
		t.Context(),
		orderedDocumentPartition,
		[]storedDocumentPageEntry{{
			key:      vault.Key("bad"),
			document: Document{NormalizedURL: "bad"},
		}},
	)
	if err != nil || len(documents) != 0 {
		t.Fatalf("malformed visible page = %#v, %v", documents, err)
	}
}

func TestStoredDocumentScanPartitionReportsVisibleCancellation(t *testing.T) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	url := "https://scanner.example/cancel"
	key, err := orderedDocumentKey(1, url)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := (documentCodec{}).Encode(Document{NormalizedURL: url})
	if err != nil {
		t.Fatal(err)
	}
	location, err := encodeOrderedDocumentAdmission(1)
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(orderedDocumentBucketName, key, raw)
	engine.putRaw(documentLocationBucketName, vault.Key(url), location)
	ctx := &errAfterContext{
		Context:   context.Background(),
		remaining: 6,
		err:       context.Canceled,
	}
	boundary := storedDocumentPartitionBoundary{
		partition: orderedDocumentPartition,
		bucket:    orderedDocumentBucketName,
		lastKey:   key,
	}
	decodeContext := &errAfterContext{
		Context:   context.Background(),
		remaining: 5,
		err:       context.Canceled,
	}
	if err := documents.scanStoredDocumentPartition(
		decodeContext,
		boundary,
		1,
		func(Document) (bool, error) { return true, nil },
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("decode scan cancellation = %v", err)
	}
	if err := documents.scanStoredDocumentPartition(
		ctx,
		boundary,
		1,
		func(Document) (bool, error) { return true, nil },
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("visible scan cancellation = %v", err)
	}
}

func TestStoredDocumentWriteBoundaryCoversLateCancellationAndNoopPaths(t *testing.T) {
	boundary := newStoredDocumentWriteBoundary()
	releaseScan, err := boundary.enterScan(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cancelContext, cancel := context.WithCancel(t.Context())
	observed := make(chan struct{}, 1)
	ctx := documentStorageObservedContext{
		Context:  cancelContext,
		observed: observed,
	}
	result := make(chan error, 1)
	go func() {
		_, err := boundary.enterWrite(ctx)
		result <- err
	}()
	<-observed
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked write cancellation = %v", err)
	}
	releaseScan()
	late := &errAfterContext{
		Context:   context.Background(),
		remaining: 1,
		err:       context.Canceled,
	}
	if _, err := boundary.enterScan(late); !errors.Is(err, context.Canceled) {
		t.Fatalf("late scan cancellation = %v", err)
	}
	empty := &storedDocumentWriteBoundary{}
	empty.ensureChanged()
	if empty.changed == nil {
		t.Fatal("empty boundary did not initialize change notification")
	}
	writeRelease, err := (documentVault{}).enterStoredDocumentWrite(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	writeRelease()
	scanRelease, err := (documentVault{}).enterStoredDocumentScanBoundary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	scanRelease()
	cancelled, cancelBoundary := context.WithCancel(t.Context())
	cancelBoundary()
	if _, err := (documentVault{writeBoundary: boundary}).enterStoredDocumentScanBoundary(
		cancelled,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("wrapped scan cancellation = %v", err)
	}
}

func TestPlanStoredDocumentWritesReportsReadValidationAndAdmissionFailures(t *testing.T) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	malformedURL := "https://plan.example/malformed"
	engine.putRaw(documentLocationBucketName, vault.Key(malformedURL), []byte{1})
	if _, err := documents.planStoredDocumentWrites(t.Context(), []Document{{
		NormalizedURL: malformedURL,
	}}); err == nil {
		t.Fatal("malformed planned location was accepted")
	}
	oversizedURL := strings.Repeat("x", 1<<16)
	if _, err := documents.planStoredDocumentWrites(t.Context(), []Document{{
		NormalizedURL: oversizedURL,
	}}); err == nil {
		t.Fatal("oversized planned URL was accepted")
	}
	engine.putRaw(documentAdmissionBucketName, documentAdmissionHighWaterKey, []byte{1})
	if _, err := documents.planStoredDocumentWrites(t.Context(), []Document{{
		NormalizedURL: "https://plan.example/admission",
	}}); err == nil {
		t.Fatal("admission read failure was ignored")
	}
}
