package crawledpagedocument

import (
	"github.com/nikitakarpei/yacy-rwi-node/searchdocument"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

func Of(page yacycrawlcontract.CrawledPage) searchdocument.Document {
	return searchdocument.Document{
		Title:     page.Title,
		URL:       page.CanonicalURL,
		Content:   page.Text,
		CrawledAt: page.CrawledAt,
		Language:  page.Language,
	}
}
