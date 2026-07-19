package crawlresults

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestDocumentFromIngestPreservesExtractionGeneration(t *testing.T) {
	t.Parallel()

	document := documentFromIngest(yagocrawlcontract.DocumentIngest{
		ExtractionGeneration: yagocrawlcontract.CurrentExtractionGeneration,
		NormalizedURL:        "https://example.test/",
	})
	if document.ExtractionGeneration != yagocrawlcontract.CurrentExtractionGeneration {
		t.Fatalf("extraction generation = %d", document.ExtractionGeneration)
	}
}
