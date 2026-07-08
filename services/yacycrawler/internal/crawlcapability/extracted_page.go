package crawlcapability

import "time"

type ExtractedPage struct {
	CanonicalURL      string
	Title             string
	Text              string
	Language          string
	FetchedAt         time.Time
	LocalLinkCount    int
	ExternalLinkCount int
}
