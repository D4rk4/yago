package yagocrawlcontract

import "github.com/D4rk4/yago/yagomodel"

type IngestBatch struct {
	SourceURL     string
	Provenance    []byte
	ProfileHandle string
	Document      DocumentIngest
	Postings      []yagomodel.RWIPosting
	Metadata      []yagomodel.URIMetadataRow
}
