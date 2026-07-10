package documentsearch

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

// countingScanner records how many postings it fed before a visit stopped it, so
// a test can prove a scan terminated early instead of draining the whole list.
type countingScanner struct {
	postings map[yagomodel.Hash][]yagomodel.RWIPosting
	fed      int
}

func (s *countingScanner) RWICount(context.Context) (int, error) { return 0, nil }

func (s *countingScanner) RWIURLCount(context.Context, yagomodel.Hash) (int, error) {
	return 0, nil
}

func (s *countingScanner) ScanWord(
	_ context.Context,
	word yagomodel.Hash,
	visit func(yagomodel.RWIPosting) (bool, error),
) error {
	for _, entry := range s.postings[word] {
		entry.WordHash = word
		s.fed++
		keepGoing, err := visit(entry)
		if err != nil {
			return err
		}
		if !keepGoing {
			return nil
		}
	}

	return nil
}

func fivePostings(word yagomodel.Hash) map[yagomodel.Hash][]yagomodel.RWIPosting {
	return map[yagomodel.Hash][]yagomodel.RWIPosting{
		word: {
			postingEntry(word, "u1", 0, 1),
			postingEntry(word, "u2", 0, 1),
			postingEntry(word, "u3", 0, 1),
			postingEntry(word, "u4", 0, 1),
			postingEntry(word, "u5", 0, 1),
		},
	}
}

func TestScanTermTerminatesEarlyWhenNotExhaustive(t *testing.T) {
	word := hashFor("w1")
	scanner := &countingScanner{postings: fivePostings(word)}
	s := searcher{index: scanner, matchesPerTerm: 2}

	appearances, total, err := s.scanTerm(t.Context(), word, termAppearanceCriteria{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(appearances) != 2 {
		t.Fatalf("appearances = %d, want 2 (capped)", len(appearances))
	}
	if scanner.fed != 3 {
		t.Fatalf("fed = %d, want 3 (stopped one past the cap)", scanner.fed)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3 (counting stopped with the scan)", total)
	}
}

func TestScanTermCountsAllWhenExhaustive(t *testing.T) {
	word := hashFor("w1")
	scanner := &countingScanner{postings: fivePostings(word)}
	s := searcher{index: scanner, matchesPerTerm: 2}

	appearances, total, err := s.scanTerm(t.Context(), word, termAppearanceCriteria{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(appearances) != 2 {
		t.Fatalf("appearances = %d, want 2 (capped)", len(appearances))
	}
	if scanner.fed != 5 || total != 5 {
		t.Fatalf("fed=%d total=%d, want 5/5 (full count past the cap)", scanner.fed, total)
	}
}

func TestReportsTermCounts(t *testing.T) {
	if (matchReporting{mode: reportNoMatches}).reportsTermCounts() {
		t.Fatal("no-matches mode must not report term counts")
	}
	if !(matchReporting{mode: reportTermWithMostMatches}).reportsTermCounts() {
		t.Fatal("largest-term mode must report term counts")
	}
	if !(matchReporting{mode: reportRequestedTerms}).reportsTermCounts() {
		t.Fatal("requested-terms mode must report term counts")
	}
}

func TestSearchCoreCriteriaAllowsEarlyTermination(t *testing.T) {
	criteria, err := searchCoreCriteria(searchcore.Request{Terms: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	if !criteria.allowEarlyTermination {
		t.Fatal("the searchcore path drops per-term totals, so it must allow early termination")
	}
}

func TestSearchRequestCriteriaGatesEarlyTerminationOnAbstracts(t *testing.T) {
	noAbstracts, err := searchCriteriaFromRequest(yagoproto.SearchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !noAbstracts.allowEarlyTermination {
		t.Fatal("a no-abstracts peer request never reads per-term totals")
	}
	auto, err := searchCriteriaFromRequest(
		yagoproto.SearchRequest{Abstracts: yagoproto.SearchAbstractsAuto},
	)
	if err != nil {
		t.Fatal(err)
	}
	if auto.allowEarlyTermination {
		t.Fatal("an abstracts request reports exact totals, so it must stay exhaustive")
	}
}
