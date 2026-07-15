package documentstore

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestReceiveStoresNewDocumentsInAdmissionOrder(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	receipt, err := receiver.Receive(t.Context(), []Document{
		{NormalizedURL: "https://example.org/d"},
		{NormalizedURL: "https://example.org/b"},
		{NormalizedURL: "https://example.org/e"},
	})
	if err != nil || receipt.Stored != 3 {
		t.Fatalf("receive = %#v, %v", receipt, err)
	}
	if len(engine.buckets[bucketName]) != 0 || len(engine.buckets[orderedDocumentBucketName]) != 3 {
		t.Fatalf(
			"legacy/ordered rows = %d/%d",
			len(engine.buckets[bucketName]),
			len(engine.buckets[orderedDocumentBucketName]),
		)
	}
	for index, normalizedURL := range []string{
		"https://example.org/d",
		"https://example.org/b",
		"https://example.org/e",
	} {
		admission := scriptedDocumentAdmission(t, engine, normalizedURL)
		if admission != uint64(index+1) {
			t.Fatalf("admission for %s = %d, want %d", normalizedURL, admission, index+1)
		}
		key, err := orderedDocumentKey(admission, normalizedURL)
		if err != nil {
			t.Fatal(err)
		}
		if engine.buckets[orderedDocumentBucketName][string(key)] == nil {
			t.Fatalf("ordered row for %s is missing", normalizedURL)
		}
	}
}

func TestReceiveReservesAdmissionsOnlyForUniqueMissingURLs(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	url := "https://example.org/existing"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: url}}); err != nil {
		t.Fatal(err)
	}
	documents := receiver.(documentVault)
	initialHighWater := readStoredDocumentAdmissionHighWaterForTest(t, documents)
	updates := make([]Document, 512)
	for index := range updates {
		updates[index] = Document{NormalizedURL: url, Title: fmt.Sprintf("update-%d", index)}
	}
	receipt, err := receiver.Receive(t.Context(), updates)
	if err != nil || receipt.Updated != len(updates) || receipt.Stored != 0 {
		t.Fatalf("updates = %#v, %v", receipt, err)
	}
	if documents.admissionKeys.issued != 1 ||
		readStoredDocumentAdmissionHighWaterForTest(t, documents) != initialHighWater {
		t.Fatalf(
			"update admissions = %d/%d, want 1/%d",
			documents.admissionKeys.issued,
			readStoredDocumentAdmissionHighWaterForTest(t, documents),
			initialHighWater,
		)
	}
	duplicateURL := "https://example.org/duplicate"
	duplicates := make([]Document, 300)
	for index := range duplicates {
		duplicates[index] = Document{
			NormalizedURL: duplicateURL,
			Title:         fmt.Sprintf("copy-%d", index),
		}
	}
	receipt, err = receiver.Receive(t.Context(), duplicates)
	if err != nil || receipt.Stored != 1 || receipt.Updated != len(duplicates)-1 {
		t.Fatalf("duplicates = %#v, %v", receipt, err)
	}
	if documents.admissionKeys.issued != 2 ||
		len(engine.buckets[orderedDocumentBucketName]) != 2 {
		t.Fatalf(
			"duplicate admissions/rows = %d/%d",
			documents.admissionKeys.issued,
			len(engine.buckets[orderedDocumentBucketName]),
		)
	}
}

func TestPhaseBFailureCleansUnpublishedRowAndReingestRepairs(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	url := "https://example.org/orphan"
	engine.putErrors[documentLocationBucketName] = errors.New("publish failed")
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: url}}); err == nil {
		t.Fatal("phase B failure was not returned")
	}
	delete(engine.putErrors, documentLocationBucketName)
	if len(engine.buckets[orderedDocumentBucketName]) != 0 ||
		len(engine.buckets[documentLocationBucketName]) != 0 {
		t.Fatalf(
			"unpublished rows/locations = %d/%d",
			len(engine.buckets[orderedDocumentBucketName]),
			len(engine.buckets[documentLocationBucketName]),
		)
	}
	assertDocumentMissing(t, directory, url)
	if count, err := directory.Count(t.Context()); err != nil || count != 0 {
		t.Fatalf("orphan count = %d, %v", count, err)
	}
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: url,
		Title:         "repaired",
	}}); err != nil {
		t.Fatalf("repair receive: %v", err)
	}
	if len(engine.buckets[orderedDocumentBucketName]) != 1 ||
		scriptedDocumentAdmission(t, engine, url) != 2 {
		t.Fatalf(
			"repaired rows/admission = %d/%d",
			len(engine.buckets[orderedDocumentBucketName]),
			scriptedDocumentAdmission(t, engine, url),
		)
	}
	assertStoredDocumentTitle(t, directory, url, "repaired")
	if count, err := directory.Count(t.Context()); err != nil || count != 1 {
		t.Fatalf("repaired count = %d, %v", count, err)
	}
}

func TestCrashOrphanRemainsInvisibleAndReadScansDoNotMutateIt(t *testing.T) {
	directory, _, engine := openScriptedDocuments(t)
	url := "https://example.org/crash-orphan"
	seedScriptedOrderedDocument(
		t,
		engine,
		1,
		url,
		[]byte(`{"NormalizedURL":"https://example.org/crash-orphan"}`),
	)
	if count, err := directory.Count(t.Context()); err != nil || count != 0 {
		t.Fatalf("crash orphan count = %d, %v", count, err)
	}
	if len(engine.buckets[orderedDocumentBucketName]) != 1 {
		t.Fatalf(
			"read scan mutated %d crash orphan rows",
			len(engine.buckets[orderedDocumentBucketName]),
		)
	}
}

func TestPhaseBCommitErrorPreservesPublishedRow(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	url := "https://example.org/committed-publication"
	engine.putCommitErrors[documentLocationBucketName] = errors.New("commit outcome unknown")
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: url}}); err == nil {
		t.Fatal("ambiguous publication outcome was not returned")
	}
	delete(engine.putCommitErrors, documentLocationBucketName)
	if len(engine.buckets[orderedDocumentBucketName]) != 1 {
		t.Fatalf("published rows = %d, want 1", len(engine.buckets[orderedDocumentBucketName]))
	}
	assertStoredDocumentTitle(t, directory, url, "")
}

func TestFailedPublicationCleanupKeepsPublishedRowsOnly(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	first := "https://example.org/published"
	second := "https://example.org/unpublished"
	seedScriptedOrderedDocument(
		t,
		engine,
		1,
		first,
		[]byte(`{"NormalizedURL":"https://example.org/published"}`),
	)
	seedScriptedOrderedDocument(
		t,
		engine,
		2,
		second,
		[]byte(`{"NormalizedURL":"https://example.org/unpublished"}`),
	)
	seedScriptedDocumentLocation(t, engine, first, 1)
	publicationError := errors.New("partial publication")
	err := documents.recoverFailedDocumentPublication(
		t.Context(),
		map[string]storedDocumentLocationPublication{
			first:  {admission: 1},
			second: {admission: 2},
		},
		publicationError,
	)
	if !errors.Is(err, publicationError) {
		t.Fatalf("cleanup error = %v", err)
	}
	firstKey, err := orderedDocumentKey(1, first)
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := orderedDocumentKey(2, second)
	if err != nil {
		t.Fatal(err)
	}
	if engine.buckets[orderedDocumentBucketName][string(firstKey)] == nil ||
		engine.buckets[orderedDocumentBucketName][string(secondKey)] != nil {
		t.Fatalf("ordered rows after cleanup = %#v", engine.buckets[orderedDocumentBucketName])
	}
}

func TestPublishDocumentLocationRejectsMissingExpectedPreviousLocation(t *testing.T) {
	_, receiver, _ := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	err := documents.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return documents.publishDocumentLocation(
			tx,
			"https://example.org/replaced",
			storedDocumentLocationPublication{admission: 2, previousAdmission: 1},
		)
	})
	if err == nil {
		t.Fatal("missing expected previous location was accepted")
	}
}

type orderedDocumentDeleteRetryCase struct {
	name         string
	failedBucket vault.Name
	shadowLegacy bool
	firstRemoved bool
}

func TestDeleteOrderedDocumentRetriesRowAndLocationFailures(t *testing.T) {
	tests := []orderedDocumentDeleteRetryCase{
		{name: "ordered row", failedBucket: orderedDocumentBucketName},
		{
			name:         "shadow legacy",
			failedBucket: bucketName,
			shadowLegacy: true,
		},
		{
			name:         "location",
			failedBucket: documentLocationBucketName,
			firstRemoved: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertOrderedDocumentDeleteRetry(t, test)
		})
	}
}

func assertOrderedDocumentDeleteRetry(
	t *testing.T,
	test orderedDocumentDeleteRetryCase,
) {
	t.Helper()
	directory, receiver, engine := openScriptedDocuments(t)
	url := "https://example.org/delete-" + test.name
	if _, err := receiver.Receive(
		t.Context(),
		[]Document{{NormalizedURL: url}},
	); err != nil {
		t.Fatal(err)
	}
	if test.shadowLegacy {
		raw, err := (documentCodec{}).Encode(Document{NormalizedURL: url})
		if err != nil {
			t.Fatal(err)
		}
		engine.buckets[bucketName][url] = raw
	}
	engine.delErrors[test.failedBucket] = errors.New("delete phase failed")
	removed, err := directory.(DocumentEvictor).Delete(t.Context(), url)
	if err == nil || removed != test.firstRemoved {
		t.Fatalf("failed delete = %t, %v", removed, err)
	}
	delete(engine.delErrors, test.failedBucket)
	removed, err = directory.(DocumentEvictor).Delete(t.Context(), url)
	if err != nil || !removed {
		t.Fatalf("retried delete = %t, %v", removed, err)
	}
	removed, err = directory.(DocumentEvictor).Delete(t.Context(), url)
	if err != nil || removed {
		t.Fatalf("final delete = %t, %v", removed, err)
	}
}

func TestPartiallyPublishedPhaseBShowsOnlyLocatedRows(t *testing.T) {
	directory, _, engine := openScriptedDocuments(t)
	first := "https://example.org/first"
	second := "https://example.org/second"
	seedScriptedOrderedDocument(
		t,
		engine,
		1,
		first,
		[]byte(`{"NormalizedURL":"https://example.org/first"}`),
	)
	seedScriptedOrderedDocument(
		t,
		engine,
		2,
		second,
		[]byte(`{"NormalizedURL":"https://example.org/second"}`),
	)
	seedScriptedDocumentLocation(t, engine, first, 1)
	assertStoredDocumentTitle(t, directory, first, "")
	assertDocumentMissing(t, directory, second)
	if count, err := directory.Count(t.Context()); err != nil || count != 1 {
		t.Fatalf("partial publication count = %d, %v", count, err)
	}
}

func TestMissingOrderedRowIsRepairedWithFreshAdmission(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	url := "https://example.org/missing-row"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: url}}); err != nil {
		t.Fatal(err)
	}
	firstAdmission := scriptedDocumentAdmission(t, engine, url)
	firstKey, err := orderedDocumentKey(firstAdmission, url)
	if err != nil {
		t.Fatal(err)
	}
	delete(engine.buckets[orderedDocumentBucketName], string(firstKey))
	assertDocumentMissing(t, directory, url)
	if found, err := directory.(DocumentPresence).DocumentExists(
		t.Context(),
		url,
	); err != nil ||
		found {
		t.Fatalf("missing-row presence = %t, %v", found, err)
	}
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: url,
		Title:         "replacement",
	}}); err != nil {
		t.Fatal(err)
	}
	if got := scriptedDocumentAdmission(t, engine, url); got <= firstAdmission {
		t.Fatalf("replacement admission = %d, want > %d", got, firstAdmission)
	}
	assertStoredDocumentTitle(t, directory, url, "replacement")
}

func TestWrongURLOrderedLocationIsRepairedWithFreshAdmission(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	url := "https://example.org/requested"
	otherURL := "https://example.org/other"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: url}}); err != nil {
		t.Fatal(err)
	}
	admission := scriptedDocumentAdmission(t, engine, url)
	originalKey, err := orderedDocumentKey(admission, url)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := orderedDocumentKey(admission, otherURL)
	if err != nil {
		t.Fatal(err)
	}
	raw := engine.buckets[orderedDocumentBucketName][string(originalKey)]
	delete(engine.buckets[orderedDocumentBucketName], string(originalKey))
	engine.buckets[orderedDocumentBucketName][string(otherKey)] = raw
	if found, err := directory.(DocumentPresence).DocumentExists(
		t.Context(),
		url,
	); err != nil ||
		found {
		t.Fatalf("wrong-key presence = %t, %v", found, err)
	}
	assertDocumentMissing(t, directory, url)
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: url,
		Title:         "replacement",
	}}); err != nil {
		t.Fatal(err)
	}
	if got := scriptedDocumentAdmission(t, engine, url); got <= admission {
		t.Fatalf("replacement admission = %d, want > %d", got, admission)
	}
	assertStoredDocumentTitle(t, directory, url, "replacement")
	assertDocumentMissing(t, directory, otherURL)
	if count, err := directory.Count(t.Context()); err != nil || count != 1 {
		t.Fatalf("wrong-key repair count = %d, %v", count, err)
	}
}

func TestMalformedOrderedDocumentRawPresenceAndRepair(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	url := "https://example.org/malformed-ordered"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: url}}); err != nil {
		t.Fatal(err)
	}
	admission := scriptedDocumentAdmission(t, engine, url)
	key, err := orderedDocumentKey(admission, url)
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[orderedDocumentBucketName][string(key)] = []byte("{")
	if found, err := directory.(DocumentPresence).DocumentExists(
		t.Context(),
		url,
	); err != nil ||
		!found {
		t.Fatalf("malformed raw presence = %t, %v", found, err)
	}
	assertDocumentMissing(t, directory, url)
	if count, err := directory.Count(t.Context()); err != nil || count != 0 {
		t.Fatalf("malformed raw count = %d, %v", count, err)
	}
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: url,
		Title:         "replacement",
	}}); err != nil {
		t.Fatal(err)
	}
	if scriptedDocumentAdmission(t, engine, url) <= admission {
		t.Fatal("malformed row admission was reused")
	}
	assertStoredDocumentTitle(t, directory, url, "replacement")
}

func TestMalformedLegacyDocumentMovesToOrderedPartition(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	url := "https://example.org/malformed-legacy"
	engine.buckets[bucketName][url] = []byte("{")
	if found, err := directory.(DocumentPresence).DocumentExists(
		t.Context(),
		url,
	); err != nil ||
		!found {
		t.Fatalf("malformed legacy presence = %t, %v", found, err)
	}
	assertDocumentMissing(t, directory, url)
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: url,
		Title:         "replacement",
	}}); err != nil {
		t.Fatal(err)
	}
	if engine.buckets[bucketName][url] == nil ||
		len(engine.buckets[orderedDocumentBucketName]) != 1 {
		t.Fatalf(
			"legacy/ordered repair rows = %d/%d",
			len(engine.buckets[bucketName]),
			len(engine.buckets[orderedDocumentBucketName]),
		)
	}
	assertStoredDocumentTitle(t, directory, url, "replacement")
	if count, err := directory.Count(t.Context()); err != nil || count != 1 {
		t.Fatalf("legacy repair count = %d, %v", count, err)
	}
	removed, err := directory.(DocumentEvictor).Delete(t.Context(), url)
	if err != nil || !removed {
		t.Fatalf("repaired legacy delete = %t, %v", removed, err)
	}
	if found, err := directory.(DocumentPresence).DocumentExists(
		t.Context(),
		url,
	); err != nil ||
		found {
		t.Fatalf("deleted repaired legacy presence = %t, %v", found, err)
	}
	if count, err := directory.Count(t.Context()); err != nil || count != 0 {
		t.Fatalf("deleted repaired legacy count = %d, %v", count, err)
	}
	removed, err = directory.(DocumentEvictor).Delete(t.Context(), url)
	if err != nil || removed {
		t.Fatalf("second repaired legacy delete = %t, %v", removed, err)
	}
}

func TestWrongURLLegacyDocumentMovesToOrderedPartition(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	url := "https://example.org/legacy-key"
	otherURL := "https://example.org/legacy-payload"
	raw, err := (documentCodec{}).Encode(Document{
		NormalizedURL: otherURL,
		Title:         "wrong",
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[bucketName][url] = raw
	if found, err := directory.(DocumentPresence).DocumentExists(
		t.Context(),
		url,
	); err != nil ||
		!found {
		t.Fatalf("wrong legacy raw presence = %t, %v", found, err)
	}
	assertDocumentMissing(t, directory, url)
	canonical, err := receiver.(CanonicalDocumentDirectory).CanonicalDocuments(
		t.Context(),
		[]Document{{NormalizedURL: url, Title: "candidate"}},
	)
	if err != nil || len(canonical) != 1 || canonical[0].Title != "candidate" {
		t.Fatalf("wrong legacy canonical = %#v, %v", canonical, err)
	}
	if count, err := directory.Count(t.Context()); err != nil || count != 0 {
		t.Fatalf("wrong legacy count = %d, %v", count, err)
	}
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: url,
		Title:         "replacement",
	}}); err != nil {
		t.Fatal(err)
	}
	if engine.buckets[bucketName][url] == nil ||
		len(engine.buckets[orderedDocumentBucketName]) != 1 {
		t.Fatalf(
			"wrong legacy repair rows = %d/%d",
			len(engine.buckets[bucketName]),
			len(engine.buckets[orderedDocumentBucketName]),
		)
	}
	assertStoredDocumentTitle(t, directory, url, "replacement")
}

func TestLegacyUpgradeUpdatesAndDeletesWithoutLengthMutation(t *testing.T) {
	engine := newScriptedDocumentEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Provision(bucketName); err != nil {
		t.Fatal(err)
	}
	url := "https://example.org/legacy"
	raw, err := (documentCodec{}).Encode(Document{NormalizedURL: url, Title: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[bucketName][url] = raw
	var legacyLength [8]byte
	binary.BigEndian.PutUint64(legacyLength[:], 1)
	engine.buckets[vault.Name("__lengths__")][string(bucketName)] = legacyLength[:]
	directory, receiver, err := Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: url,
		Title:         "updated",
	}}); err != nil {
		t.Fatal(err)
	}
	if len(engine.buckets[orderedDocumentBucketName]) != 0 ||
		len(engine.buckets[documentLocationBucketName]) != 0 {
		t.Fatalf(
			"legacy update created ordered state %d/%d",
			len(engine.buckets[orderedDocumentBucketName]),
			len(engine.buckets[documentLocationBucketName]),
		)
	}
	assertStoredDocumentTitle(t, directory, url, "updated")
	if got := engine.buckets[vault.Name("__lengths__")][string(bucketName)]; binary.BigEndian.Uint64(
		got,
	) != 1 {
		t.Fatalf("legacy length after update = %x", got)
	}
	removed, err := directory.(DocumentEvictor).Delete(t.Context(), url)
	if err != nil || !removed {
		t.Fatalf("legacy delete = %t, %v", removed, err)
	}
	if got := engine.buckets[vault.Name("__lengths__")][string(bucketName)]; binary.BigEndian.Uint64(
		got,
	) != 1 {
		t.Fatalf("legacy length after delete = %x", got)
	}
}

func TestSameURLConcurrentReceiveKeepsOneAuthoritativeRow(t *testing.T) {
	directory, receiver, engine := openPagedDocuments(t)
	url := "https://example.org/concurrent"
	const writers = 32
	start := make(chan struct{})
	errorsFound := make(chan error, writers)
	var group sync.WaitGroup
	for sequence := range writers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := receiver.Receive(t.Context(), []Document{{
				NormalizedURL: url,
				Title:         fmt.Sprintf("writer-%d", sequence),
			}})
			errorsFound <- err
		}()
	}
	close(start)
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	engine.mu.RLock()
	orderedRows := len(engine.buckets[orderedDocumentBucketName])
	locations := len(engine.buckets[documentLocationBucketName])
	engine.mu.RUnlock()
	if orderedRows != 1 || locations != 1 {
		t.Fatalf("ordered rows/locations = %d/%d", orderedRows, locations)
	}
	if count, err := directory.Count(t.Context()); err != nil || count != 1 {
		t.Fatalf("concurrent count = %d, %v", count, err)
	}
	if documents := receiver.(documentVault); documents.admissionKeys.issued != 1 {
		t.Fatalf("issued admissions = %d, want 1", documents.admissionKeys.issued)
	}
}

func TestPointReadWaitsUntilLocationPublication(t *testing.T) {
	directory, receiver, engine := openPagedDocuments(t)
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: "https://example.org/seed",
	}}); err != nil {
		t.Fatal(err)
	}
	url := "https://example.org/published"
	phaseBEntered := make(chan struct{})
	releasePhaseB := make(chan struct{})
	var updates atomic.Int64
	engine.beforeUpdate = func() {
		if updates.Add(1) == 2 {
			close(phaseBEntered)
			<-releasePhaseB
		}
	}
	writeDone := make(chan error, 1)
	go func() {
		_, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: url}})
		writeDone <- err
	}()
	<-phaseBEntered
	type pointRead struct {
		document Document
		found    bool
		err      error
	}
	readDone := make(chan pointRead, 1)
	go func() {
		document, found, err := directory.Document(t.Context(), url)
		readDone <- pointRead{document: document, found: found, err: err}
	}()
	select {
	case read := <-readDone:
		t.Fatalf("point read crossed publication: %#v", read)
	case <-time.After(50 * time.Millisecond):
	}
	close(releasePhaseB)
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	read := <-readDone
	if read.err != nil || !read.found || read.document.NormalizedURL != url {
		t.Fatalf("published point read = %#v", read)
	}
}

func scriptedDocumentAdmission(
	t *testing.T,
	engine *scriptedDocumentEngine,
	normalizedURL string,
) uint64 {
	t.Helper()
	admission, err := decodeOrderedDocumentAdmission(
		engine.buckets[documentLocationBucketName][normalizedURL],
	)
	if err != nil {
		t.Fatalf("decode location for %s: %v", normalizedURL, err)
	}

	return admission
}

func seedScriptedDocumentLocation(
	t *testing.T,
	engine *scriptedDocumentEngine,
	normalizedURL string,
	admission uint64,
) {
	t.Helper()
	raw, err := encodeOrderedDocumentAdmission(admission)
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[documentLocationBucketName][normalizedURL] = raw
}

func seedScriptedOrderedDocument(
	t *testing.T,
	engine *scriptedDocumentEngine,
	admission uint64,
	normalizedURL string,
	raw []byte,
) {
	t.Helper()
	key, err := orderedDocumentKey(admission, normalizedURL)
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[orderedDocumentBucketName][string(key)] = append([]byte(nil), raw...)
}

func assertDocumentMissing(
	t *testing.T,
	directory DocumentDirectory,
	normalizedURL string,
) {
	t.Helper()
	document, found, err := directory.Document(t.Context(), normalizedURL)
	if err != nil || found || document.NormalizedURL != "" {
		t.Fatalf("missing document = %#v, %t, %v", document, found, err)
	}
}

func assertStoredDocumentTitle(
	t *testing.T,
	directory DocumentDirectory,
	normalizedURL string,
	title string,
) {
	t.Helper()
	document, found, err := directory.Document(t.Context(), normalizedURL)
	if err != nil || !found || document.NormalizedURL != normalizedURL || document.Title != title {
		t.Fatalf("stored document = %#v, %t, %v", document, found, err)
	}
}
