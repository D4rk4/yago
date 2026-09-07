package searchlocal

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestPageEvidencePreservesCompletedRowsAlongsideDeadline(t *testing.T) {
	inner := pageEvidenceInner{
		response: searchcore.Response{Results: []searchcore.Result{{
			DocumentID: "document", URL: "https://example.org/page", Source: searchcore.SourceLocal,
		}}},
		err: context.DeadlineExceeded,
	}
	response, err := NewPageEvidenceSearcher(inner, rejectingPageEvidenceSource{}).Search(
		t.Context(), searchcore.Request{Query: "synthetic"},
	)
	if !errors.Is(err, context.DeadlineExceeded) || len(response.Results) != 1 {
		t.Fatalf("response=%+v error=%v", response, err)
	}
}
