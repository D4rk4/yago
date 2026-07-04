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

func FromCrawledPage(text yacycrawlcontract.CrawledPage) SearchDocument {
	return SearchDocument{
		Title:     text.Title,
		URL:       text.CanonicalURL,
		Content:   text.Text,
		CrawledAt: text.CrawledAt,
		Language:  text.Language,
	}
}
