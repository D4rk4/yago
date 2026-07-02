package yacycrawlcontract

import "github.com/D4rk4/yago/yacymodel"

type IngestBatch struct {
	SourceURL     string
	Provenance    []byte
	ProfileHandle string
	Document      DocumentIngest
	Postings      []yacymodel.RWIPosting
	Metadata      []yacymodel.URIMetadataRow
}
