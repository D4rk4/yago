package documentstore

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b scriptedDocumentBucket) ReadPresence(
	ctx context.Context,
	keys []vault.Key,
) ([]bool, error) {
	b.engine.presenceMu.Lock()
	b.engine.presenceReads[b.name]++
	failure := b.engine.presenceErrors[b.name]
	b.engine.presenceMu.Unlock()
	if failure != nil {
		return nil, failure
	}
	found := make([]bool, len(keys))
	for index, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(errors.New("presence context"), err)
		}
		_, found[index] = b.engine.buckets[b.name][string(key)]
	}

	return found, nil
}

func TestDocumentSetPresenceUsesOrderedAndLegacyBatches(t *testing.T) {
	directory, receiver, engine := openScriptedDocuments(t)
	stored := "https://example.org/stored"
	missing := "https://example.org/missing"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: stored}}); err != nil {
		t.Fatal(err)
	}
	found, err := directory.(DocumentSetPresence).DocumentsExist(
		t.Context(),
		[]string{stored, missing},
	)
	if err != nil || len(found) != 2 || !found[0] || found[1] ||
		engine.presenceReads[orderedDocumentBucketName] != 1 ||
		engine.presenceReads[bucketName] != 1 {
		t.Fatalf("presence=%v reads=%v error=%v", found, engine.presenceReads, err)
	}
}

func TestDocumentSetPresenceRejectsOrderedAndLegacyBatchFailures(t *testing.T) {
	failure := errors.New("presence failed")
	cases := []struct {
		name   string
		bucket vault.Name
		url    string
		seed   bool
	}{
		{
			name:   "ordered",
			bucket: orderedDocumentBucketName,
			url:    "https://example.org/ordered",
			seed:   true,
		},
		{
			name:   "legacy",
			bucket: bucketName,
			url:    "https://example.org/legacy",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory, receiver, engine := openScriptedDocuments(t)
			if test.seed {
				if _, err := receiver.Receive(
					t.Context(),
					[]Document{{NormalizedURL: test.url}},
				); err != nil {
					t.Fatal(err)
				}
			}
			engine.presenceErrors[test.bucket] = failure
			if _, err := directory.(DocumentSetPresence).DocumentsExist(
				t.Context(),
				[]string{test.url},
			); !errors.Is(err, failure) {
				t.Fatalf("presence failure=%v, want=%v", err, failure)
			}
		})
	}
}
