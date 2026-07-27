package searchindex

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// A value that already fits comes back byte-identical and complete. The stored
// candidate projection is a lossy copy of the document kept inside the index so
// a candidate-only search never touches the vault; each field records whether it
// survived intact, and an incomplete field forces the whole hit back through the
// vault. An off-by-one in the bound would mark the largest legitimate title,
// language, or content type as truncated and quietly give up the fast path for
// every document sitting exactly on the limit.
func TestBoundedStoredCandidateStringKeepsValuesWithinTheLimitIntact(t *testing.T) {
	for _, limit := range []int{
		maximumStoredCandidateTitleBytes,
		maximumStoredCandidateLanguageBytes,
		maximumStoredCandidateContentTypeBytes,
	} {
		exact := strings.Repeat("t", limit)
		bounded, complete := boundedStoredCandidateString(exact, limit)
		if !complete || bounded != exact {
			t.Fatalf("limit %d bounded = %q complete=%t, want unchanged", limit, bounded, complete)
		}
		over := exact + "x"
		bounded, complete = boundedStoredCandidateString(over, limit)
		if complete || len(bounded) != limit {
			t.Fatalf("limit %d over-limit = %q complete=%t", limit, bounded, complete)
		}
	}
	shortCluster := strings.Repeat("c", maximumStoredCandidateClusterBytes)
	if got := boundedStoredCandidateClusterID(shortCluster); got != shortCluster {
		t.Fatalf("exact-limit cluster id = %q, want unchanged", got)
	}
}

// The completeness flags decide vault fallback, so a document sitting exactly on
// every limit must stay eligible for the candidate fast path.
func TestStoredCandidateProjectionAdmitsFiltersAtExactFieldLimits(t *testing.T) {
	projection := newStoredCandidateProjection(documentstore.Document{
		Title:             strings.Repeat("t", maximumStoredCandidateTitleBytes),
		RepresentativeURL: strings.Repeat("r", maximumStoredCandidateRepresentativeBytes),
		Language:          strings.Repeat("l", maximumStoredCandidateLanguageBytes),
		ContentType:       strings.Repeat("m", maximumStoredCandidateContentTypeBytes),
		Metadata: map[string]string{
			"author": strings.Repeat("a", maximumStoredCandidateAuthorBytes),
		},
	})
	if !projection.RepresentativeComplete || !projection.AuthorComplete ||
		!projection.LanguageComplete || !projection.ContentTypeComplete {
		t.Fatalf("exact-limit projection = %#v, want every field complete", projection)
	}
	for _, request := range []SearchRequest{
		{},
		{Author: "a"},
		{Language: "l"},
		{FileType: "pdf"},
		{ContentDomain: "audio"},
	} {
		if !projection.supports(request) {
			t.Fatalf("exact-limit projection refused the candidate path for %#v", request)
		}
	}
}
