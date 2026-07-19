package pageindex_test

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

func TestBuildDocumentStampsCurrentExtractionGeneration(t *testing.T) {
	t.Parallel()

	document := pageindex.BuildDocument(
		pageparse.ParsedPage{URL: "https://example.test/", Text: "body"},
		pageparse.PageStats{},
		yagomodel.URIMetadataRow{},
		time.Unix(1, 0),
	)
	if document.ExtractionGeneration != yagocrawlcontract.CurrentExtractionGeneration {
		t.Fatalf(
			"extraction generation = %d, want %d",
			document.ExtractionGeneration,
			yagocrawlcontract.CurrentExtractionGeneration,
		)
	}
}
