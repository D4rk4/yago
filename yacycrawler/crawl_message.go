package yacycrawler

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

type CrawlJob struct {
	URL   string
	Depth int
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

type IngestBatch struct {
	SourceURL string
	Postings  []yacymodel.RWIEntry
	Metadata  []yacymodel.URIMetadataRow
}
