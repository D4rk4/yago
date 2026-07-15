package searchindex

import (
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func BenchmarkStoredWordFormProximityEvidence(b *testing.B) {
	statement := "observatory telemetry processor was transferring its operators complete sensor archive to remote storage"
	document := documentstore.Document{
		NormalizedURL: "https://example.org/report",
		Title:         "Incident report",
		ExtractedText: strings.Repeat("background material without query evidence ", 128) +
			statement + strings.Repeat(" unrelated appendix material", 128),
		Language: "en",
	}
	request := SearchRequest{
		Query:            statement,
		Terms:            strings.Fields(statement),
		IncludePositions: true,
	}
	hit := &search.DocumentMatch{ID: "report", Score: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := searchResultFromStoredEvidence(
			b.Context(),
			hit,
			document,
			request,
		); err != nil {
			b.Fatal(err)
		}
	}
}
