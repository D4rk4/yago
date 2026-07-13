package yagocrawlcontract

import (
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type IngestBatch struct {
	SourceURL     string
	Provenance    []byte
	ProfileHandle string
	ObservationID string
	ObservedAt    time.Time
	Removed       bool
	Document      DocumentIngest
	Postings      []yagomodel.RWIPosting
	Metadata      []yagomodel.URIMetadataRow
}
