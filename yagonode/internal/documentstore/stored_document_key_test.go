package documentstore

import (
	"bytes"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
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

func TestOrderedDocumentKeyRejectsInvalidIdentity(t *testing.T) {
	if _, err := orderedDocumentKey(0, "https://example.org/"); err == nil {
		t.Fatal("zero admission accepted")
	}
	if _, err := orderedDocumentKey(1, ""); err == nil {
		t.Fatal("empty URL accepted")
	}
	tooLong := strings.Repeat("x", yagomodel.MaximumURLIdentityBytes+1)
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
