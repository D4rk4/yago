package yacycrawlcontract

import "time"

type DocumentIngest struct {
	CanonicalURL        string
	NormalizedURL       string
	Title               string
	Headings            []string
	ExtractedText       string
	RawContentReference string
	Language            string
	ContentType         string
	FetchStatus         string
	FetchedAt           time.Time
	IndexedAt           time.Time
	ContentHash         string
	Outlinks            []string
	Inlinks             []AnchorText
	Metadata            map[string]string
}

type AnchorText struct {
	URL  string
	Text string
}
