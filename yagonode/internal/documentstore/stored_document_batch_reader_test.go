package documentstore

import (
	"context"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestStoredDocumentBatchReaderContinuesAcrossLegacyAndOrderedPartitions(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	for _, normalizedURL := range []string{
		"https://example.test/legacy-a",
		"https://example.test/legacy-b",
	} {
		raw, err := (documentCodec{}).Encode(Document{NormalizedURL: normalizedURL})
		if err != nil {
			t.Fatal(err)
		}
		engine.buckets[bucketName][normalizedURL] = raw
	}
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: "https://example.test/ordered",
	}}); err != nil {
		t.Fatal(err)
	}

	reader := directory.(StoredDocumentBatchReader)
	var continuation string
	var got []string
	examined := 0
	for step := 0; step < 4; step++ {
		batch, err := reader.ReadStoredDocumentBatch(t.Context(), continuation, 1)
		if err != nil {
			t.Fatal(err)
		}
		examined += batch.Examined
		for _, document := range batch.Documents {
			got = append(got, document.NormalizedURL)
		}
		if batch.Complete {
			break
		}
		if batch.Continuation == "" {
			t.Fatal("partial batch has no continuation")
		}
		continuation = batch.Continuation
	}
	want := []string{
		"https://example.test/legacy-a",
		"https://example.test/legacy-b",
		"https://example.test/ordered",
	}
	if !slices.Equal(got, want) || examined != len(want) {
		t.Fatalf("documents = %v, examined = %d", got, examined)
	}
}

func TestStoredDocumentBatchReaderKeepsInitialHighBoundary(t *testing.T) {
	directory, receiver := openDocuments(t)
	if _, err := receiver.Receive(t.Context(), []Document{
		{NormalizedURL: "https://example.test/first"},
		{NormalizedURL: "https://example.test/second"},
		{NormalizedURL: "https://example.test/third"},
	}); err != nil {
		t.Fatal(err)
	}
	reader := directory.(StoredDocumentBatchReader)
	first, err := reader.ReadStoredDocumentBatch(t.Context(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Complete || first.Examined != 1 || len(first.Documents) != 1 {
		t.Fatalf("first batch = %+v", first)
	}
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: "https://example.test/after-boundary",
	}}); err != nil {
		t.Fatal(err)
	}
	second, err := reader.ReadStoredDocumentBatch(
		t.Context(),
		first.Continuation,
		MaximumStoredDocumentBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Complete || second.Examined != 2 || len(second.Documents) != 2 {
		t.Fatalf("second batch = %+v", second)
	}
	for _, document := range second.Documents {
		if document.NormalizedURL == "https://example.test/after-boundary" {
			t.Fatal("continuation crossed its initial high boundary")
		}
	}
}

func TestStoredDocumentBatchReaderRejectsUnboundedAndInvalidRequests(t *testing.T) {
	directory, _ := openDocuments(t)
	reader := directory.(StoredDocumentBatchReader)
	for _, limit := range []int{0, MaximumStoredDocumentBatchSize + 1} {
		if _, err := reader.ReadStoredDocumentBatch(t.Context(), "", limit); err == nil {
			t.Fatalf("limit %d was accepted", limit)
		}
	}
	if _, err := reader.ReadStoredDocumentBatch(t.Context(), "not-base64", 1); err == nil {
		t.Fatal("invalid continuation was accepted")
	}
}

func TestStoredDocumentBatchReaderRejectsInvalidContinuations(t *testing.T) {
	maximumKeyBytes := yagomodel.MaximumURLIdentityBytes + orderedDocumentAdmissionSize
	tests := []struct {
		name         string
		continuation string
		want         string
	}{
		{
			name:         "too large",
			continuation: strings.Repeat("x", maximumStoredDocumentContinuationBytes+1),
			want:         "continuation is too large",
		},
		{
			name:         "malformed base64",
			continuation: "*",
			want:         "decode stored document continuation",
		},
		{
			name:         "malformed JSON",
			continuation: base64.RawURLEncoding.EncodeToString([]byte("{")),
			want:         "parse stored document continuation",
		},
		{
			name: "unsupported version",
			continuation: formatStoredDocumentBatchPosition(storedDocumentBatchPosition{
				Version: storedDocumentBatchContinuationVersion + 1,
			}),
			want: "unsupported stored document continuation version",
		},
		{
			name: "invalid partition",
			continuation: formatStoredDocumentBatchPosition(storedDocumentBatchPosition{
				Version:   storedDocumentBatchContinuationVersion,
				Partition: orderedDocumentPartition + 1,
			}),
			want: "invalid stored document continuation partition",
		},
		{
			name: "oversized key",
			continuation: formatStoredDocumentBatchPosition(storedDocumentBatchPosition{
				Version:    storedDocumentBatchContinuationVersion,
				Partition:  legacyDocumentPartition,
				OrderedEnd: make(vault.Key, maximumKeyBytes+1),
			}),
			want: "ordered end is too large",
		},
		{
			name: "after missing boundary",
			continuation: formatStoredDocumentBatchPosition(storedDocumentBatchPosition{
				Version:   storedDocumentBatchContinuationVersion,
				Partition: legacyDocumentPartition,
				After:     vault.Key("a"),
			}),
			want: "continuation is past its boundary",
		},
		{
			name: "after past boundary",
			continuation: formatStoredDocumentBatchPosition(storedDocumentBatchPosition{
				Version:   storedDocumentBatchContinuationVersion,
				Partition: legacyDocumentPartition,
				After:     vault.Key("b"),
				LegacyEnd: vault.Key("a"),
			}),
			want: "continuation is past its boundary",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseStoredDocumentBatchPosition(test.continuation)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("continuation error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestStoredDocumentBatchReaderReportsScanAndStorageFailures(t *testing.T) {
	t.Run("scan admission", func(t *testing.T) {
		directory, _ := openDocuments(t)
		documents := directory.(documentVault)
		release, err := documents.enterStoredDocumentScan(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := documents.ReadStoredDocumentBatch(
			ctx,
			"",
			1,
		); !errors.Is(
			err,
			context.Canceled,
		) {
			t.Fatalf("scan admission error = %v", err)
		}
	})

	t.Run("boundary", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		sentinel := errors.New("last key failed")
		engine.lastKeyErrors[bucketName] = sentinel
		if _, err := documents.ReadStoredDocumentBatch(
			t.Context(),
			"",
			1,
		); !errors.Is(
			err,
			sentinel,
		) {
			t.Fatalf("boundary error = %v", err)
		}
	})

	t.Run("page", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		url := "https://batch.example/page"
		engine.putRaw(bucketName, vault.Key(url), []byte("document"))
		sentinel := errors.New("page failed")
		engine.pageErrors[bucketName] = sentinel
		if _, err := documents.ReadStoredDocumentBatch(
			t.Context(),
			"",
			1,
		); !errors.Is(
			err,
			sentinel,
		) {
			t.Fatalf("page error = %v", err)
		}
	})

	t.Run("authority", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		url := "https://batch.example/authority"
		raw, err := (documentCodec{}).Encode(Document{NormalizedURL: url})
		if err != nil {
			t.Fatal(err)
		}
		engine.putRaw(bucketName, vault.Key(url), raw)
		engine.putRaw(documentLocationBucketName, vault.Key(url), []byte{1})
		if _, err := documents.ReadStoredDocumentBatch(t.Context(), "", 1); err == nil {
			t.Fatal("authority failure was ignored")
		}
	})
}

func TestStoredDocumentBatchReaderReportsTransformationCancellation(t *testing.T) {
	_, documents, engine := openDocumentStorageFaultVault(t)
	url := "https://batch.example/cancel"
	raw, err := (documentCodec{}).Encode(Document{NormalizedURL: url})
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(bucketName, vault.Key(url), raw)
	position := storedDocumentBatchPosition{
		Version:   storedDocumentBatchContinuationVersion,
		Partition: legacyDocumentPartition,
		LegacyEnd: vault.Key(url),
	}
	for _, test := range []struct {
		name      string
		remaining int
	}{
		{name: "decode", remaining: 5},
		{name: "visibility", remaining: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := &errAfterContext{
				Context:   context.Background(),
				remaining: test.remaining,
				err:       context.Canceled,
			}
			if _, err := documents.readStoredDocumentBatch(
				ctx,
				position,
				1,
			); !errors.Is(
				err,
				context.Canceled,
			) {
				t.Fatalf("transformation cancellation error = %v", err)
			}
		})
	}
}
