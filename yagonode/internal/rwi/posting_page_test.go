package rwi

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestStoredPostingPageAdvancesExclusiveCursor(t *testing.T) {
	harness := openHarness(t, 0, 100)
	entries := []struct{ word, url string }{
		{"alpha", "one"},
		{"alpha", "two"},
		{"beta", "three"},
	}
	for _, entry := range entries {
		if _, err := harness.rwi.Receiver.Receive(
			context.Background(),
			[]yagomodel.RWIPosting{posting(entry.word, entry.url)},
		); err != nil {
			t.Fatal(err)
		}
	}

	source, ok := harness.rwi.Index.(PostingPageSource)
	if !ok {
		t.Fatal("posting page source unavailable")
	}
	first, err := source.StoredPostingPage(context.Background(), nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 2 || !first.More {
		t.Fatalf("first page = %+v", first)
	}
	if bytes.Compare(first.Entries[0].Cursor, first.Entries[1].Cursor) >= 0 {
		t.Fatalf("cursors not ordered: %q %q", first.Entries[0].Cursor, first.Entries[1].Cursor)
	}
	second, err := source.StoredPostingPage(
		context.Background(), first.Entries[1].Cursor, 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Entries) != 1 || second.More ||
		bytes.Compare(second.Entries[0].Cursor, first.Entries[1].Cursor) <= 0 {
		t.Fatalf("second page = %+v", second)
	}
}

func TestStoredPostingPageRejectsUnreadableAndInconsistentRecords(t *testing.T) {
	word := yagomodel.WordHash("word")
	urlHash := yagomodel.WordHash("url")
	otherURL := yagomodel.WordHash("other-url")
	tests := []struct {
		name      string
		key       string
		value     []byte
		readError error
		want      string
	}{
		{
			name: "read", readError: errors.New("read failed"),
			want: "read posting bucket page",
		},
		{name: "key length", key: "short", want: "stored posting key length"},
		{
			name: "word hash", key: strings.Repeat("!", yagomodel.HashLength) + urlHash.String(),
			want: "stored posting word",
		},
		{
			name: "posting", key: string(postingKey(word, urlHash)),
			want: "empty posting value",
		},
		{
			name: "missing URL", key: string(postingKey(word, urlHash)),
			value: encodeStoredPosting(yagomodel.RWIPosting{
				WordHash: word, Properties: map[string]string{},
			}),
			want: "stored posting url",
		},
		{
			name: "mismatched URL", key: string(postingKey(word, urlHash)),
			value: encodeStoredPosting(yagomodel.RWIPosting{
				WordHash:   word,
				Properties: map[string]string{yagomodel.ColURLHash: otherURL.String()},
			}),
			want: "stored posting url does not match key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage, index, _, _, engine := openScriptedRWI(t, fakeURLDirectory{})
			t.Cleanup(func() { _ = storage.Close() })
			engine.scanErrors[PostingsBucket] = test.readError
			if test.key != "" {
				engine.buckets[PostingsBucket][test.key] = append([]byte(nil), test.value...)
			}
			source, ok := index.(PostingPageSource)
			if !ok {
				t.Fatal("posting page source unavailable")
			}
			_, err := source.StoredPostingPage(t.Context(), nil, 1)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("stored page error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestStoredPostingPageRejectsNonpositiveLimit(t *testing.T) {
	storage, index, _, _, _ := openScriptedRWI(t, fakeURLDirectory{})
	t.Cleanup(func() { _ = storage.Close() })
	source, ok := index.(PostingPageSource)
	if !ok {
		t.Fatal("posting page source unavailable")
	}
	if _, err := source.StoredPostingPage(t.Context(), nil, 0); err == nil ||
		!strings.Contains(err.Error(), "limit must be positive") {
		t.Fatalf("nonpositive page limit error = %v", err)
	}
}
