package yacycrawlcontract

import (
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type CrawledPageIndex struct {
	CanonicalURL string
	Postings     []yacymodel.RWIPosting
	Metadata     []yacymodel.URIMetadataRow
}
