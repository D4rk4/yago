package yagocrawlcontract

import "github.com/D4rk4/yago/yagomodel"

type IngestBatch struct {
	SourceURL     string
	Provenance    []byte
	ProfileHandle string
	// Removed marks a tombstone batch: a recrawl found SourceURL permanently
	// gone (HTTP 404/410), so the node purges its index entry rather than
	// storing one. A tombstone carries only SourceURL, Provenance, and
	// ProfileHandle; Document, Postings, and Metadata are empty (ADR-0034).
	Removed  bool
	Document DocumentIngest
	Postings []yagomodel.RWIPosting
	Metadata []yagomodel.URIMetadataRow
}
