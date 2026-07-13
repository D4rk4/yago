package documentsearch

import (
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestSearchCriteriaAppliesReceiverLimits(t *testing.T) {
	tests := []struct {
		name      string
		count     int
		time      int
		wantCount int
		wantTime  time.Duration
	}{
		{name: "defaults", wantCount: 10, wantTime: 3 * time.Second},
		{name: "smaller", count: 4, time: 250, wantCount: 4, wantTime: 250 * time.Millisecond},
		{name: "maximum", count: 10, time: 3000, wantCount: 10, wantTime: 3 * time.Second},
		{name: "above maximum", count: 11, time: 3001, wantCount: 10, wantTime: 3 * time.Second},
		{name: "negative", count: -1, time: -1, wantCount: 10, wantTime: 3 * time.Second},
		{
			name:      "largest integer",
			count:     int(^uint(0) >> 1),
			time:      int(^uint(0) >> 1),
			wantCount: 10,
			wantTime:  3 * time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			criteria, err := searchCriteriaFromRequest(yagoproto.SearchRequest{
				Count: test.count,
				Time:  test.time,
			})
			if err != nil {
				t.Fatal(err)
			}
			if criteria.maxResults != test.wantCount || criteria.timeLimit != test.wantTime {
				t.Fatalf(
					"limits = %d/%s, want %d/%s",
					criteria.maxResults,
					criteria.timeLimit,
					test.wantCount,
					test.wantTime,
				)
			}
		})
	}
}

func TestEndpointClampsPeerRequestedCount(t *testing.T) {
	word := hashFor("w1")
	postings := make([]yagomodel.RWIPosting, 0, 11)
	identifiers := make([]string, 0, 11)
	for i := range 11 {
		identifier := fmt.Sprintf("u%d", i)
		postings = append(postings, postingEntry(word, identifier, 0, 1))
		identifiers = append(identifiers, identifier)
	}
	endpoint := newEndpoint(
		fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{word: postings}},
		fakeDirectory{rows: urlRows(identifiers...)},
	)

	resp := serveSearch(t, endpoint, yagoproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yagomodel.Hash{word},
		Count:       100,
	})
	if resp.Count != remoteSearchMaximumCount {
		t.Fatalf("Count = %d, want %d", resp.Count, remoteSearchMaximumCount)
	}
}
