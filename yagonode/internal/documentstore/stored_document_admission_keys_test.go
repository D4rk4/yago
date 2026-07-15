package documentstore

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOpenStoredDocumentAdmissionKeysRecoversPhysicalHighKey(t *testing.T) {
	tests := []struct {
		name      string
		persisted uint64
		physical  uint64
		want      uint64
	}{
		{name: "physical", persisted: 7, physical: 11, want: 11},
		{name: "durable", persisted: 13, physical: 11, want: 13},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := newPagedDocumentEngine()
			seedPagedAdmissionHighWater(t, engine, test.persisted)
			seedPagedOrderedDocument(t, engine, test.physical, "https://example.org/high")
			_, directory, _ := openPagedDocumentsOnEngine(t, engine)
			documents := directory.(documentVault)
			if documents.admissionKeys.issued != test.want ||
				documents.admissionKeys.reserved != test.want {
				t.Fatalf(
					"local admissions = %d/%d, want %d",
					documents.admissionKeys.issued,
					documents.admissionKeys.reserved,
					test.want,
				)
			}
			if got := readStoredDocumentAdmissionHighWaterForTest(t, documents); got != test.want {
				t.Fatalf("durable admission = %d, want %d", got, test.want)
			}
		})
	}
}

func TestOpenStoredDocumentAdmissionKeysDoesNotRegressConcurrentHighWater(t *testing.T) {
	engine := newPagedDocumentEngine()
	seedPagedAdmissionHighWater(t, engine, 5)
	seedPagedOrderedDocument(t, engine, 10, "https://example.org/high")
	var raise sync.Once
	engine.beforeUpdate = func() {
		raise.Do(func() {
			seedPagedAdmissionHighWater(t, engine, 20)
		})
	}
	_, directory, _ := openPagedDocumentsOnEngine(t, engine)
	documents := directory.(documentVault)
	if documents.admissionKeys.issued != 20 || documents.admissionKeys.reserved != 20 {
		t.Fatalf(
			"local admissions = %d/%d, want 20",
			documents.admissionKeys.issued,
			documents.admissionKeys.reserved,
		)
	}
	if got := readStoredDocumentAdmissionHighWaterForTest(t, documents); got != 20 {
		t.Fatalf("durable admission = %d, want 20", got)
	}
}

func TestStoredDocumentAdmissionKeysRefillDoesNotReadOrderedLastKey(t *testing.T) {
	directory, _, engine := openPagedDocuments(t)
	documents := directory.(documentVault)
	first, err := documents.admissionKeys.issue(t.Context(), storedDocumentAdmissionReservation)
	if err != nil || first[len(first)-1] != storedDocumentAdmissionReservation {
		t.Fatalf("first reservation = %#v, %v", first, err)
	}
	engine.lastKeyReads.Store(0)
	engine.lastKeyError = errors.New("runtime last-key read")
	second, err := documents.admissionKeys.issue(t.Context(), 1)
	if err != nil || len(second) != 1 || second[0] != 257 {
		t.Fatalf("second reservation = %#v, %v", second, err)
	}
	if engine.lastKeyReads.Load() != 0 {
		t.Fatalf("runtime last-key reads = %d", engine.lastKeyReads.Load())
	}
}

func TestStoredDocumentAdmissionKeysReservesEveryIssuedKey(t *testing.T) {
	directory, _, _ := openPagedDocuments(t)
	documents := directory.(documentVault)
	first, err := documents.admissionKeys.issue(t.Context(), 156)
	if err != nil || first[0] != 1 || first[len(first)-1] != 156 {
		t.Fatalf("first issue = %#v, %v", first, err)
	}
	second, err := documents.admissionKeys.issue(t.Context(), 300)
	if err != nil || second[0] != 257 || second[len(second)-1] != 556 {
		t.Fatalf("second issue bounds = %d..%d, %v", second[0], second[len(second)-1], err)
	}
	if documents.admissionKeys.issued > documents.admissionKeys.reserved {
		t.Fatalf(
			"issued %d exceeds reserved %d",
			documents.admissionKeys.issued,
			documents.admissionKeys.reserved,
		)
	}
	if got := readStoredDocumentAdmissionHighWaterForTest(t, documents); got != 556 {
		t.Fatalf("durable admission = %d, want 556", got)
	}
}

func TestStoredDocumentAdmissionKeysReservationRetryIsIdempotent(t *testing.T) {
	for _, commitFirst := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "rollback", true: "committed"}[commitFirst],
			func(t *testing.T) {
				_, receiver, engine := openScriptedDocuments(t)
				documents := receiver.(documentVault)
				engine.replayNext = true
				engine.commitFirst = commitFirst
				issued, err := documents.admissionKeys.issue(t.Context(), 300)
				if err != nil || issued[0] != 1 || issued[len(issued)-1] != 300 {
					t.Fatalf("issued bounds = %d..%d, %v", issued[0], issued[len(issued)-1], err)
				}
				if got := readStoredDocumentAdmissionHighWaterForTest(t, documents); got != 300 {
					t.Fatalf("durable admission = %d, want 300", got)
				}
			},
		)
	}
}

func TestStoredDocumentAdmissionKeysSkipAmbiguousCommittedReservation(t *testing.T) {
	tests := []struct {
		name   string
		reopen bool
	}{
		{name: "same allocator"},
		{name: "reopened allocator", reopen: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertAmbiguousStoredDocumentAdmissionReservation(t, test.reopen)
		})
	}
}

func assertAmbiguousStoredDocumentAdmissionReservation(
	t *testing.T,
	reopen bool,
) {
	t.Helper()
	_, receiver, engine := openScriptedDocuments(t)
	documents := receiver.(documentVault)
	engine.commitError = errors.New("reservation commit outcome unknown")
	if _, err := documents.admissionKeys.issue(t.Context(), 1); err == nil {
		t.Fatal("ambiguous reservation outcome was not returned")
	}
	if documents.admissionKeys.issued != 0 || documents.admissionKeys.reserved != 0 {
		t.Fatalf(
			"failed reservation changed local state to %d/%d",
			documents.admissionKeys.issued,
			documents.admissionKeys.reserved,
		)
	}
	if reopen {
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatal(err)
		}
		_, reopened, err := Open(storage)
		if err != nil {
			t.Fatal(err)
		}
		documents = reopened.(documentVault)
	}
	issued, err := documents.admissionKeys.issue(t.Context(), 1)
	if err != nil || len(issued) != 1 || issued[0] != 257 {
		t.Fatalf("post-ambiguity issue = %#v, %v", issued, err)
	}
}

func TestStoredDocumentAdmissionKeysRejectsPartialBlockOverflow(t *testing.T) {
	engine := newPagedDocumentEngine()
	seedPagedAdmissionHighWater(t, engine, math.MaxUint64-512)
	_, directory, _ := openPagedDocumentsOnEngine(t, engine)
	documents := directory.(documentVault)
	issued, err := documents.admissionKeys.issue(t.Context(), 156)
	if err != nil || issued[len(issued)-1] != math.MaxUint64-356 {
		t.Fatalf("partial issue tail = %d, %v", issued[len(issued)-1], err)
	}
	previousIssued := documents.admissionKeys.issued
	previousReserved := documents.admissionKeys.reserved
	if _, err := documents.admissionKeys.issue(t.Context(), 300); err == nil {
		t.Fatal("overflowing reservation succeeded")
	}
	if documents.admissionKeys.issued != previousIssued ||
		documents.admissionKeys.reserved != previousReserved {
		t.Fatalf(
			"failed reservation changed state from %d/%d to %d/%d",
			previousIssued,
			previousReserved,
			documents.admissionKeys.issued,
			documents.admissionKeys.reserved,
		)
	}
}

func TestStoredDocumentAdmissionKeysExhaustsAtMaximum(t *testing.T) {
	engine := newPagedDocumentEngine()
	seedPagedAdmissionHighWater(t, engine, math.MaxUint64-256)
	_, directory, _ := openPagedDocumentsOnEngine(t, engine)
	documents := directory.(documentVault)
	issued, err := documents.admissionKeys.issue(t.Context(), 256)
	if err != nil || issued[len(issued)-1] != math.MaxUint64 {
		t.Fatalf("maximum issue tail = %d, %v", issued[len(issued)-1], err)
	}
	if _, err := documents.admissionKeys.issue(t.Context(), 1); err == nil {
		t.Fatal("post-maximum reservation succeeded")
	}
	if got := readStoredDocumentAdmissionHighWaterForTest(t, documents); got != math.MaxUint64 {
		t.Fatalf("durable admission = %d, want maximum", got)
	}
}

func TestStoredDocumentAdmissionKeysRestartSkipsUnusedReservation(t *testing.T) {
	engine := newPagedDocumentEngine()
	_, firstDirectory, _ := openPagedDocumentsOnEngine(t, engine)
	first := firstDirectory.(documentVault)
	issued, err := first.admissionKeys.issue(t.Context(), 1)
	if err != nil || issued[0] != 1 {
		t.Fatalf("first issue = %#v, %v", issued, err)
	}
	_, secondDirectory, _ := openPagedDocumentsOnEngine(t, engine)
	second := secondDirectory.(documentVault)
	issued, err = second.admissionKeys.issue(t.Context(), 1)
	if err != nil || issued[0] != 257 {
		t.Fatalf("restart issue = %#v, %v", issued, err)
	}
}

func TestStoredDocumentAdmissionKeysRecoveredMaximumSurvivesDeletion(t *testing.T) {
	engine := newPagedDocumentEngine()
	seedPagedAdmissionHighWater(t, engine, 5)
	key := seedPagedOrderedDocument(t, engine, 10, "https://example.org/high")
	firstVault, firstDirectory, _ := openPagedDocumentsOnEngine(t, engine)
	first := firstDirectory.(documentVault)
	if err := firstVault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := first.orderedDocuments.Delete(tx, key)
		if err != nil {
			return fmt.Errorf("delete recovered high document: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, secondDirectory, _ := openPagedDocumentsOnEngine(t, engine)
	second := secondDirectory.(documentVault)
	issued, err := second.admissionKeys.issue(t.Context(), 1)
	if err != nil || issued[0] != 11 {
		t.Fatalf("post-deletion issue = %#v, %v", issued, err)
	}
}

func seedPagedAdmissionHighWater(
	t *testing.T,
	engine *pagedDocumentEngine,
	highWater uint64,
) {
	t.Helper()
	if err := engine.Provision(documentAdmissionBucketName); err != nil {
		t.Fatal(err)
	}
	raw, err := encodeOrderedDocumentAdmission(highWater)
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[documentAdmissionBucketName][string(documentAdmissionHighWaterKey)] = raw
}

func seedPagedOrderedDocument(
	t *testing.T,
	engine *pagedDocumentEngine,
	admission uint64,
	normalizedURL string,
) vault.Key {
	t.Helper()
	if err := engine.Provision(orderedDocumentBucketName); err != nil {
		t.Fatal(err)
	}
	key, err := orderedDocumentKey(admission, normalizedURL)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := (documentCodec{}).Encode(Document{NormalizedURL: normalizedURL})
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[orderedDocumentBucketName][string(key)] = raw

	return key
}

func readStoredDocumentAdmissionHighWaterForTest(
	t *testing.T,
	documents documentVault,
) uint64 {
	t.Helper()
	var highWater uint64
	err := documents.vault.View(t.Context(), func(tx *vault.Txn) error {
		stored, found, err := documents.admissionKeys.admissions.Get(
			tx,
			documentAdmissionHighWaterKey,
		)
		if err != nil {
			return fmt.Errorf("read document admission high water: %w", err)
		}
		if !found {
			return errors.New("document admission high water is missing")
		}
		highWater = stored

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return highWater
}
