package yacycrawlcontract

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

type IngestBatch struct {
	SourceURL     string
	Provenance    []byte
	ProfileHandle string
	Postings      []yacymodel.RWIEntry
	Metadata      []yacymodel.URIMetadataRow
}
