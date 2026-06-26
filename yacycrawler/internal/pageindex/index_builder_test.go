package pageindex_test

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlwork"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageparse"
)

func TestIndexBuilderBuildsPostingsAndMetadata(t *testing.T) {
	page := crawlwork.ParsedPage{
		URL:      "http://example.com/path",
		Title:    "Kangaroo facts",
		Language: "en",
		Text:     "kangaroo hops across the outback",
	}
	artifacts, err := pageindex.NewIndexBuilder().Build(page, pageparse.BuildPageStats(page))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(artifacts.Postings) == 0 {
		t.Error("expected postings")
	}
	if len(artifacts.Metadata.Properties) == 0 {
		t.Error("expected metadata properties")
	}
}
