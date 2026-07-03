package searchdocument_test

import (
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/searchdocument"
)

func TestFromExtractedTextMapsFields(t *testing.T) {
	crawledAt := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	doc := searchdocument.FromExtractedText(yacycrawlcontract.ExtractedText{
		CanonicalURL: "https://example.com/",
		Title:        "Hi",
		Text:         "words here",
		CrawledAt:    crawledAt,
		Language:     "en",
	})
	if doc.URL != "https://example.com/" || doc.Title != "Hi" || doc.Content != "words here" ||
		!doc.CrawledAt.Equal(crawledAt) || doc.Language != "en" {
		t.Errorf("document = %+v", doc)
	}
}
