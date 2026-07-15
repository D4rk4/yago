package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peernews"
)

type fakePeerNewsReader struct {
	records []peernews.Record
	err     error
}

func (f fakePeerNewsReader) Recent(
	context.Context,
	peernews.Queue,
	int,
) ([]peernews.Record, error) {
	return f.records, f.err
}

func fixedPeerNewsNow() time.Time {
	return time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
}

func TestPeerNewsSourceMapsRecords(t *testing.T) {
	created := fixedPeerNewsNow().Add(-3 * time.Hour)
	reader := fakePeerNewsReader{records: []peernews.Record{{
		Originator: yagomodel.WordHash("peer"),
		Created:    created,
		Received:   created,
		Category:   peernews.CategoryCrawlStart,
		Attributes: map[string]string{"startURL": "http://example.test/"},
	}}}
	source := newPeerNewsSource(reader)
	source.now = fixedPeerNewsNow

	items, available := source.PeerNews(context.Background())
	if !available {
		t.Fatal("successful peer-news read should be available")
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	switch {
	case items[0].Category != peernews.CategoryCrawlStart:
		t.Fatalf("category = %q", items[0].Category)
	case items[0].Age != "3h":
		t.Fatalf("age = %q, want 3h", items[0].Age)
	case items[0].Detail != "startURL=http://example.test/":
		t.Fatalf("detail = %q", items[0].Detail)
	case items[0].Originator == "":
		t.Fatal("originator missing")
	}
}

func TestPeerNewsSourceReportsUnavailableOnError(t *testing.T) {
	source := newPeerNewsSource(fakePeerNewsReader{err: errors.New("read failed")})
	items, available := source.PeerNews(context.Background())
	if available || items != nil {
		t.Fatalf("PeerNews = %v/%v, want unavailable on error", items, available)
	}
}

func TestPeerNewsAgeHumanizes(t *testing.T) {
	now := fixedPeerNewsNow()
	cases := map[string]struct {
		stamp time.Time
		want  string
	}{
		"minutes": {now.Add(-30 * time.Minute), "30m"},
		"hours":   {now.Add(-5 * time.Hour), "5h"},
		"days":    {now.Add(-50 * time.Hour), "2d"},
		"future":  {now.Add(time.Hour), ""},
	}
	for name, tc := range cases {
		if got := peerNewsAge(peernews.Record{Received: tc.stamp}, now); got != tc.want {
			t.Fatalf("%s: age = %q, want %q", name, got, tc.want)
		}
	}
	if got := peerNewsAge(peernews.Record{Created: now.Add(-90 * time.Minute)}, now); got != "1h" {
		t.Fatalf("created-fallback age = %q, want 1h", got)
	}
	if got := peerNewsAge(peernews.Record{Created: now.Add(time.Hour)}, now); got != "" {
		t.Fatalf("future created age = %q, want unavailable", got)
	}
	if got := peerNewsAge(peernews.Record{}, now); got != "" {
		t.Fatalf("no-stamp age = %q, want empty", got)
	}
}

func TestPeerNewsDetailEmpty(t *testing.T) {
	if got := peerNewsDetail(nil); got != "" {
		t.Fatalf("detail(nil) = %q, want empty", got)
	}
}
