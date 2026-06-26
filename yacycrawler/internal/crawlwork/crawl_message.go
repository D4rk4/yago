package crawlwork

import (
	"github.com/google/uuid"
)

type CrawlJob struct {
	URL           string
	Depth         int
	ProfileHandle string
	Provenance    []byte
	RunID         uuid.UUID
}

type ParsedPage struct {
	URL      string
	Title    string
	Language string
	Text     string
	Links    []string
}

type PageStats struct {
	Tokens        []string
	TitleTokens   []string
	LocalLinks    []string
	ExternalLinks []string
}
