package searchdocument

import (
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type SearchDocument struct {
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Content   string    `json:"content"`
	CrawledAt time.Time `json:"crawled_at"`
	Language  string    `json:"language"`
}

func FromCrawledPage(page yacycrawlcontract.CrawledPage) SearchDocument {
	return SearchDocument{
		Title:     page.Title,
		URL:       page.CanonicalURL,
		Content:   page.Text,
		CrawledAt: page.CrawledAt,
		Language:  page.Language,
	}
}
