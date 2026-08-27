package searchindex

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type requestedHitSearchProbe struct {
	bleveIndexContract
	hits      []*search.DocumentMatch
	requests  []int
	searchErr error
	errorAt   int
}

func (p *requestedHitSearchProbe) DocCount() (uint64, error) {
	return uint64(len(p.hits)), nil
}

func (p *requestedHitSearchProbe) SearchInContext(
	_ context.Context,
	request *bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	p.requests = append(p.requests, request.Size)
	if p.searchErr != nil && len(p.requests) == p.errorAt {
		return nil, p.searchErr
	}
	end := min(request.Size, len(p.hits))

	return &bleve.SearchResult{
		Status: &bleve.SearchStatus{Total: 1, Successful: 1},
		Total:  uint64(len(p.hits)),
		Hits:   append(search.DocumentMatchCollection(nil), p.hits[:end]...),
	}, nil
}

func TestRequestedHitSearchUsesExactHealthyPage(t *testing.T) {
	documents := storedCandidatePageDocuments(8)
	directory := newBatchPresenceDocumentDirectory(documents)
	probe := requestedHitProbe(t, documents)
	index := requestedHitIndex(probe, directory)

	result, orphans, err := index.searchHits(t.Context(), SearchRequest{
		Query: "candidate", MaxResults: 3, CandidateOnly: true,
	})
	if err != nil || len(orphans) != 0 || result.Total != 8 {
		t.Fatalf("result=%#v orphans=%v error=%v", result, orphans, err)
	}
	if !slices.Equal(probe.requests, []int{3}) {
		t.Fatalf("search sizes=%v, want [3]", probe.requests)
	}
	if want := documentIdentities(
		documents[:3],
	); !slices.Equal(
		searchResultIdentities(result.Results),
		want,
	) {
		t.Fatalf("result identities=%v, want=%v", searchResultIdentities(result.Results), want)
	}
}

func TestRequestedHitSearchExpandsOnlyForOrphanedTail(t *testing.T) {
	documents := storedCandidatePageDocuments(8)
	directory := newBatchPresenceDocumentDirectory(documents)
	delete(directory.documents, documents[1].NormalizedURL)
	probe := requestedHitProbe(t, documents)
	index := requestedHitIndex(probe, directory)

	result, orphans, err := index.searchHits(t.Context(), SearchRequest{
		Query: "candidate", MaxResults: 3, CandidateOnly: true,
	})
	if err != nil || result.Total != 7 ||
		!slices.Equal(orphans, []string{documents[1].NormalizedURL}) {
		t.Fatalf("result=%#v orphans=%v error=%v", result, orphans, err)
	}
	if !slices.Equal(probe.requests, []int{3, 8}) {
		t.Fatalf("search sizes=%v, want [3 8]", probe.requests)
	}
	want := []string{
		documents[0].NormalizedURL,
		documents[2].NormalizedURL,
		documents[3].NormalizedURL,
	}
	if !slices.Equal(searchResultIdentities(result.Results), want) {
		t.Fatalf("result identities=%v, want=%v", searchResultIdentities(result.Results), want)
	}
}

func TestRequestedHitSearchDoesNotExpandWithoutTail(t *testing.T) {
	documents := storedCandidatePageDocuments(3)
	directory := newBatchPresenceDocumentDirectory(documents)
	delete(directory.documents, documents[1].NormalizedURL)
	probe := requestedHitProbe(t, documents)
	index := requestedHitIndex(probe, directory)

	result, orphans, err := index.searchHits(t.Context(), SearchRequest{
		Query: "candidate", MaxResults: 3, CandidateOnly: true,
	})
	if err != nil || result.Total != 2 || len(result.Results) != 2 || len(orphans) != 1 {
		t.Fatalf("result=%#v orphans=%v error=%v", result, orphans, err)
	}
	if !slices.Equal(probe.requests, []int{3}) {
		t.Fatalf("search sizes=%v, want [3]", probe.requests)
	}
}

func TestRequestedHitSearchReturnsExpandedPageFailure(t *testing.T) {
	documents := storedCandidatePageDocuments(5)
	directory := newBatchPresenceDocumentDirectory(documents)
	delete(directory.documents, documents[0].NormalizedURL)
	sentinel := errors.New("expanded search failed")
	probe := requestedHitProbe(t, documents)
	probe.searchErr = sentinel
	probe.errorAt = 2
	index := requestedHitIndex(probe, directory)

	_, _, err := index.searchHits(t.Context(), SearchRequest{
		Query: "candidate", MaxResults: 2, CandidateOnly: true,
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expanded search error=%v, want=%v", err, sentinel)
	}
	if !slices.Equal(probe.requests, []int{2, 5}) {
		t.Fatalf("search sizes=%v, want [2 5]", probe.requests)
	}
}

func TestRequestedHitSearchReturnsExpandedPageCancellation(t *testing.T) {
	documents := storedCandidatePageDocuments(5)
	directory := newBatchPresenceDocumentDirectory(documents)
	delete(directory.documents, documents[0].NormalizedURL)
	probe := requestedHitProbe(t, documents)
	probe.searchErr = context.Canceled
	probe.errorAt = 2
	index := requestedHitIndex(probe, directory)

	_, _, err := index.searchHits(t.Context(), SearchRequest{
		Query: "candidate", MaxResults: 2, CandidateOnly: true,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expanded search error=%v, want=%v", err, context.Canceled)
	}
}

func TestDiskRequestedSearchSize(t *testing.T) {
	for name, test := range map[string]struct {
		maximumResults int
		documents      int
		want           int
	}{
		"negative":  {-1, 10, 0},
		"zero":      {0, 10, 0},
		"exact":     {5, 100, 5},
		"cap":       {2000, 3000, bleveSearchHitCap},
		"documents": {500, 7, 7},
	} {
		t.Run(name, func(t *testing.T) {
			if got := diskRequestedSearchSize(
				test.maximumResults,
				test.documents,
			); got != test.want {
				t.Fatalf("disk requested search size=%d, want=%d", got, test.want)
			}
		})
	}
}

func requestedHitProbe(
	t *testing.T,
	documents []documentstore.Document,
) *requestedHitSearchProbe {
	t.Helper()

	return &requestedHitSearchProbe{hits: storedCandidateSetHits(t, documents...)}
}

func requestedHitIndex(
	probe *requestedHitSearchProbe,
	directory *batchPresenceDocumentDirectory,
) *BleveDiskIndex {
	return &BleveDiskIndex{
		shards:           []bleve.Index{probe},
		alias:            probe,
		documents:        directory,
		documentPresence: directory,
		storedCandidates: true,
	}
}
