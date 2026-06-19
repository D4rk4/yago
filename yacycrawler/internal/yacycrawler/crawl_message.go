package yacycrawler

import (
	"github.com/google/uuid"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type IngestBatch = yacycrawlcontract.IngestBatch

type CrawlJob struct {
	URL           string
	Depth         int
	ProfileHandle string
	Provenance    []byte
	RunID         uuid.UUID
}

type FetchedPage struct {
	URL         string
	ContentType string
	Body        []byte
}

type ParsedPage struct {
	URL      string
	Title    string
	Language string
	Text     string
	Links    []string
}
