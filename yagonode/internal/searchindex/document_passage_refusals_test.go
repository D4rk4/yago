package searchindex

import (
	"fmt"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type documentPassageRefusal struct {
	name   string
	mutate func(*DocumentPassageRequest)
	want   string
}

// Every passage refusal must name itself. The request validator answers eight
// different questions with one error type, and a caller that only learns "the
// request was invalid" cannot tell an operator whether the term budget, the
// analyzer, or the byte range was at fault. Asserting the message keeps each
// guard load-bearing: a request carrying one term too many must be turned away
// by the term budget, not incidentally by a later check that happens to fire on
// the same input.
func TestDocumentPassageValidationNamesEveryRefusal(t *testing.T) {
	doc := documentstore.Document{NormalizedURL: "doc", ExtractedText: "é term"}
	for _, refusal := range documentPassageRefusals(doc) {
		t.Run(refusal.name, func(t *testing.T) {
			request := DocumentPassageRequest{
				DocumentID: "doc", Analyzer: "en", Terms: []string{"term"}, Start: 0, End: 1,
			}
			refusal.mutate(&request)
			_, err := documentPassage(t.Context(), doc, request)
			if err == nil || err.Error() != refusal.want {
				t.Fatalf("passage refusal = %v, want %q", err, refusal.want)
			}
		})
	}
}

func documentPassageRefusals(doc documentstore.Document) []documentPassageRefusal {
	const (
		wantID       = "document passage id required"
		wantTerms    = "document passage terms required"
		wantAnalyzer = "document passage analyzer invalid"
		wantTerm     = "document passage term invalid"
		wantRange    = "document passage range invalid"
		wantContext  = "document passage context invalid"
		wantSplit    = "document passage range splits UTF-8 text"
	)
	wantBudget := fmt.Sprintf(
		"document passage terms exceed %d",
		maximumDocumentPassageTerms,
	)
	refuse := func(
		name string,
		want string,
		mutate func(*DocumentPassageRequest),
	) documentPassageRefusal {
		return documentPassageRefusal{name: name, want: want, mutate: mutate}
	}

	return []documentPassageRefusal{
		refuse("blank id", wantID,
			func(r *DocumentPassageRequest) { r.DocumentID = " " }),
		refuse("unreadable id", wantID,
			func(r *DocumentPassageRequest) { r.DocumentID = "bad\xff" }),
		refuse("no terms", wantTerms,
			func(r *DocumentPassageRequest) { r.Terms = nil }),
		refuse("blank analyzer", wantAnalyzer,
			func(r *DocumentPassageRequest) { r.Analyzer = " " }),
		refuse("unregistered analyzer", wantAnalyzer,
			func(r *DocumentPassageRequest) { r.Analyzer = "unknown" }),
		refuse("term budget", wantBudget, func(r *DocumentPassageRequest) {
			r.Terms = distinctPassageTerms(maximumDocumentPassageTerms + 1)
		}),
		refuse("blank term", wantTerm,
			func(r *DocumentPassageRequest) { r.Terms = []string{" "} }),
		refuse("unreadable term", wantTerm,
			func(r *DocumentPassageRequest) { r.Terms = []string{"bad\xff"} }),
		refuse("negative start", wantRange,
			func(r *DocumentPassageRequest) { r.Start = -1 }),
		refuse("empty range", wantRange,
			func(r *DocumentPassageRequest) { r.End = 0 }),
		refuse("range past the document", wantRange,
			func(r *DocumentPassageRequest) { r.End = len(doc.ExtractedText) + 1 }),
		refuse("negative context", wantContext,
			func(r *DocumentPassageRequest) { r.SurroundingRunes = -1 }),
		refuse("context budget", wantContext, func(r *DocumentPassageRequest) {
			r.SurroundingRunes = maximumDocumentPassageSurroundingRunes + 1
		}),
		refuse("range splitting a rune", wantSplit, func(r *DocumentPassageRequest) {
			r.Start, r.End = 1, 2
		}),
	}
}

// The bounds admit their own limit. A request carrying exactly the term budget
// or exactly the surrounding-context budget is the largest legitimate one a
// caller can build, so an off-by-one in either guard would silently refuse the
// widest passage the API promises.
func TestDocumentPassageValidationAdmitsExactBudgets(t *testing.T) {
	doc := documentstore.Document{
		NormalizedURL: "doc",
		ExtractedText: strings.Repeat("term filler ", 200),
	}
	passage, err := documentPassage(t.Context(), doc, DocumentPassageRequest{
		DocumentID: doc.NormalizedURL,
		Analyzer:   "en",
		Terms:      distinctPassageTerms(maximumDocumentPassageTerms),
		Start:      0,
		End:        len(doc.ExtractedText),
	})
	if err != nil || passage.Text == "" {
		t.Fatalf("exact term budget passage = %#v, error = %v", passage, err)
	}
	passage, err = documentPassage(t.Context(), doc, DocumentPassageRequest{
		DocumentID:       doc.NormalizedURL,
		Analyzer:         "en",
		Terms:            []string{"term"},
		Start:            len("term filler "),
		End:              len("term filler term"),
		SurroundingRunes: maximumDocumentPassageSurroundingRunes,
	})
	if err != nil || passage.Start != 0 {
		t.Fatalf("exact context budget passage = %#v, error = %v", passage, err)
	}
}

func distinctPassageTerms(count int) []string {
	terms := make([]string, count)
	for index := range terms {
		terms[index] = fmt.Sprintf("term%d", index)
	}

	return terms
}
