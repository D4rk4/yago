package yacycrawlcontract

import "time"

type ExtractedText struct {
	CanonicalURL string
	DocumentID   string
	Title        string
	Text         string
	CrawledAt    time.Time
	Language     string
}
