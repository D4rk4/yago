package documentstore

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOrderedDocumentKeyRoundTripAndOrder(t *testing.T) {
	first, err := orderedDocumentKey(7, "https://a.example/")
	if err != nil {
		t.Fatal(err)
	}
	secondURL, err := orderedDocumentKey(7, "https://b.example/")
	if err != nil {
		t.Fatal(err)
	}
	secondAdmission, err := orderedDocumentKey(8, "https://a.example/")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(first, secondURL) >= 0 || bytes.Compare(secondURL, secondAdmission) >= 0 {
		t.Fatalf("ordered keys = %x, %x, %x", first, secondURL, secondAdmission)
	}
	admission, normalizedURL, err := decodeOrderedDocumentKey(first)
	if err != nil || admission != 7 || normalizedURL != "https://a.example/" {
		t.Fatalf("decoded key = %d, %q, %v", admission, normalizedURL, err)
	}
}

func TestBboltDocumentStoreRoundTripsMigrationSizedURLIdentity(t *testing.T) {
	storage, err := boltvault.Open(filepath.Join(t.TempDir(), "documents.db"), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	directory, receiver, err := Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	urlPrefix := "https://a.example.test/"
	longURL := urlPrefix + strings.Repeat("a", maximumStoredDocumentURLIdentityBytes-len(urlPrefix))
	shortURL := "https://b.example.test/"
	if _, err := receiver.Receive(t.Context(), []Document{
		{NormalizedURL: longURL, Title: "imported long identity"},
		{NormalizedURL: shortURL, Title: "short identity"},
	}); err != nil {
		t.Fatalf("store migration-sized document identities in bbolt: %v", err)
	}
	stored, found, err := directory.Document(t.Context(), longURL)
	if err != nil || !found || stored.NormalizedURL != longURL {
		t.Fatalf("read migration-sized identity = %#v, %t, %v", stored, found, err)
	}
	reader := directory.(StoredDocumentBatchReader)
	first, err := reader.ReadStoredDocumentBatch(t.Context(), "", 1)
	if err != nil || first.Complete || len(first.Documents) != 1 ||
		first.Documents[0].NormalizedURL != longURL {
		t.Fatalf("first oversized-identity export batch = %#v, %v", first, err)
	}
	second, err := reader.ReadStoredDocumentBatch(t.Context(), first.Continuation, 1)
	if err != nil || !second.Complete || len(second.Documents) != 1 ||
		second.Documents[0].NormalizedURL != shortURL {
		t.Fatalf("continued oversized-identity export batch = %#v, %v", second, err)
	}
}

func TestOrderedDocumentKeyRejectsInvalidIdentity(t *testing.T) {
	if _, err := orderedDocumentKey(0, "https://example.org/"); err == nil {
		t.Fatal("zero admission accepted")
	}
	if _, err := orderedDocumentKey(1, ""); err == nil {
		t.Fatal("empty URL accepted")
	}
	tooLong := strings.Repeat("x", maximumStoredDocumentURLIdentityBytes+1)
	if _, err := orderedDocumentKey(1, tooLong); err == nil {
		t.Fatal("oversized URL accepted")
	}
	if _, _, err := decodeOrderedDocumentKey(vault.Key("short")); err == nil {
		t.Fatal("short ordered key accepted")
	}
	zeroAdmission := make(vault.Key, orderedDocumentAdmissionSize+1)
	zeroAdmission[len(zeroAdmission)-1] = 'x'
	if _, _, err := decodeOrderedDocumentKey(zeroAdmission); err == nil {
		t.Fatal("zero ordered key admission accepted")
	}
	oversized, err := encodeOrderedDocumentAdmission(1)
	if err != nil {
		t.Fatal(err)
	}
	oversized = append(oversized, tooLong...)
	if _, _, err := decodeOrderedDocumentKey(oversized); err == nil {
		t.Fatal("oversized ordered key URL accepted")
	}
}

func TestDocumentLocationCodecRoundTripAndValidation(t *testing.T) {
	codec := documentLocationCodec{}
	raw, err := codec.Encode(42)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(raw)
	if err != nil || decoded != 42 {
		t.Fatalf("decoded location = %d, %v", decoded, err)
	}
	if _, err := codec.Encode(0); err == nil {
		t.Fatal("zero location encoded")
	}
	if _, err := codec.Decode(nil); err == nil {
		t.Fatal("empty location decoded")
	}
}
