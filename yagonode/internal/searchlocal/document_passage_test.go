package searchlocal

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type localDocumentPassageIndex struct {
	*fakeIndex
	passage searchindex.DocumentPassage
	found   bool
	err     error
}

func (i *localDocumentPassageIndex) DocumentPassage(
	context.Context,
	searchindex.DocumentPassageRequest,
) (searchindex.DocumentPassage, bool, error) {
	return i.passage, i.found, i.err
}

type pageDocumentPassageSource struct {
	pageEvidenceSource
	passage searchindex.DocumentPassage
	found   bool
	err     error
}

func (s *pageDocumentPassageSource) DocumentPassage(
	context.Context,
	searchindex.DocumentPassageRequest,
) (searchindex.DocumentPassage, bool, error) {
	return s.passage, s.found, s.err
}

func TestLocalSearcherExposesRelativeDocumentPassageMatches(t *testing.T) {
	index := &localDocumentPassageIndex{
		fakeIndex: &fakeIndex{},
		found:     true,
		passage: searchindex.DocumentPassage{
			Text:  "полномочий",
			Start: 10,
			End:   32,
			QueryMatches: []searchindex.TextQueryMatch{{
				Start: 0,
				End:   len("полномочий"),
			}},
		},
	}
	searcher := NewSearcher(index)
	passages, ok := searcher.(searchcore.DocumentPassageSearcher)
	if !ok {
		t.Fatal("local searcher lost document passage surface")
	}
	passage, found, err := passages.DocumentPassage(
		t.Context(),
		searchcore.DocumentPassageRequest{
			DocumentID: "document",
			Analyzer:   "ru",
			Terms:      []string{"полномочия"},
			Start:      10,
			End:        32,
		},
	)
	if err != nil || !found || passage.Text != "полномочий" ||
		len(passage.QueryMatches) != 1 || passage.QueryMatches[0].Start != 0 ||
		passage.QueryMatches[0].End != len("полномочий") {
		t.Fatalf("passage=%#v found=%t error=%v", passage, found, err)
	}
}

func TestPageEvidenceSearcherPreservesDocumentPassageSurface(t *testing.T) {
	source := &pageDocumentPassageSource{
		found:   true,
		passage: searchindex.DocumentPassage{Text: "target", End: 6},
	}
	searcher := NewPageEvidenceSearcher(pageEvidenceInner{}, source)
	passages, ok := searcher.(searchcore.DocumentPassageSearcher)
	if !ok {
		t.Fatal("page evidence searcher lost document passage surface")
	}
	passage, found, err := passages.DocumentPassage(
		t.Context(),
		searchcore.DocumentPassageRequest{DocumentID: "document"},
	)
	if err != nil || !found || passage.Text != "target" {
		t.Fatalf("passage=%#v found=%t error=%v", passage, found, err)
	}
}

func TestDocumentPassageSurfaceReportsUnavailableAndSourceErrors(t *testing.T) {
	req := searchcore.DocumentPassageRequest{DocumentID: "document"}
	if _, _, err := (localSearcher{index: &fakeIndex{}}).DocumentPassage(
		t.Context(),
		req,
	); err == nil {
		t.Fatal("unsupported local index served a passage")
	}
	unsupported := pageEvidenceSearcher{
		inner: pageEvidenceInner{}, evidence: &pageEvidenceSource{},
	}
	if _, _, err := unsupported.DocumentPassage(t.Context(), req); err == nil {
		t.Fatal("unsupported page evidence served a passage")
	}
	sentinel := errors.New("read failed")
	index := &localDocumentPassageIndex{fakeIndex: &fakeIndex{}, err: sentinel}
	if _, _, err := (localSearcher{index: index}).DocumentPassage(
		t.Context(),
		req,
	); !errors.Is(err, sentinel) {
		t.Fatalf("source error = %v", err)
	}
}

func TestCoreBodyQueryMatchesPreservesNilAndEmptyEvidence(t *testing.T) {
	if coreBodyQueryMatches(nil) != nil {
		t.Fatal("nil body evidence became authoritative")
	}
	empty := coreBodyQueryMatches([]searchindex.TextQueryMatch{})
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty body evidence = %#v", empty)
	}
	mapped := coreBodyQueryMatches([]searchindex.TextQueryMatch{{Start: 3, End: 7}})
	if len(mapped) != 1 || mapped[0] != (searchcore.QueryMatch{Start: 3, End: 7}) {
		t.Fatalf("mapped body evidence = %#v", mapped)
	}
}
