package searchlocal

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func TestLongStatementRanksCompactOrderedDocument(t *testing.T) {
	query := "the observatory telemetry processor was transferring its operators complete sensor archive to remote storage"
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://alpha-records.net/analysis",
			Title:         "Technical record",
			ExtractedText: "remote background the context storage review observatory notes archive appendix telemetry reference sensor material processor report complete timeline operators evidence its summary transferring neutral was details to closing",
			Language:      "en",
		},
		{
			NormalizedURL: "https://middle-report.dev/analysis",
			Title:         "Technical record",
			ExtractedText: "storage remote to archive sensor complete operators its transferring was processor telemetry observatory the background context review notes appendix reference material report timeline evidence summary neutral details closing",
			Language:      "en",
		},
		{
			NormalizedURL: "https://zebra-research.org/analysis",
			Title:         "Technical record",
			ExtractedText: "background context review notes appendix reference material the observatory telemetry processor was transferring its operators complete sensor archive to remote storage report timeline evidence summary neutral details closing",
			Language:      "en",
		},
	}

	assertLocalRelevanceOrder(t, query, documents, []string{
		documents[2].NormalizedURL,
	})
}

func TestMorphologyProximityPreservesSurfaceAndCompactness(t *testing.T) {
	query := "connections rotating"
	documents := []documentstore.Document{
		{
			NormalizedURL: "https://alpha-scattered.dev/record",
			Title:         "Technical note",
			ExtractedText: "background connected context notes rotates",
			Language:      "en",
		},
		{
			NormalizedURL: "https://middle-compact.net/record",
			Title:         "Technical note",
			ExtractedText: "background connected rotates context notes",
			Language:      "en",
		},
		{
			NormalizedURL: "https://zebra-exact.org/record",
			Title:         "Technical note",
			ExtractedText: "background connections rotating context notes",
			Language:      "en",
		},
	}

	assertLocalRelevanceOrder(t, query, documents, []string{
		documents[2].NormalizedURL,
		documents[1].NormalizedURL,
		documents[0].NormalizedURL,
	})
}

func assertLocalRelevanceOrder(
	t *testing.T,
	query string,
	documents []documentstore.Document,
	want []string,
) {
	t.Helper()
	index, err := searchindex.NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}
	for _, document := range documents {
		if err := index.Index(t.Context(), document); err != nil {
			t.Fatalf("Index(%s): %v", document.NormalizedURL, err)
		}
	}
	response, err := searchcore.NewLexicalRerankSearcher(NewSearcher(index)).Search(
		t.Context(),
		searchcore.Request{
			Query: query,
			Terms: strings.Fields(query),
			Limit: len(documents),
		},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != len(documents) {
		t.Fatalf(
			"result count = %d, want %d: %#v",
			len(response.Results),
			len(documents),
			response.Results,
		)
	}
	for index, expectedURL := range want {
		result := response.Results[index]
		if result.URL != expectedURL {
			t.Fatalf(
				"result %d = %q, want %q; ranked results = %#v",
				index,
				result.URL,
				expectedURL,
				response.Results,
			)
		}
	}
}
