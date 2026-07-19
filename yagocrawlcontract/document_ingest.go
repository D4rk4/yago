package yagocrawlcontract

import "time"

type DocumentIngest struct {
	ExtractionGeneration        uint64 `json:",omitempty"`
	CanonicalURL                string
	NormalizedURL               string
	Title                       string
	Headings                    []string
	ExtractedText               string
	RawContentReference         string
	Language                    string
	ContentType                 string
	FetchStatus                 string
	FetchedAt                   time.Time
	IndexedAt                   time.Time
	PublishedAt                 time.Time
	ModifiedAt                  time.Time
	FirstSeenAt                 time.Time
	ContentChangedAt            time.Time
	DateConfidence              float64
	DateSource                  string
	ContentHash                 string
	Outlinks                    []string
	Inlinks                     []AnchorText
	OutboundAnchors             []OutboundAnchor
	OutboundAnchorEvidenceKnown bool
	SafetyLabels                SafetyLabels
	Images                      []ImageMetadata
	Metadata                    map[string]string
}

type AnchorText struct {
	URL           string
	Text          string
	NoFollow      bool
	UserGenerated bool
	Sponsored     bool
}

type OutboundAnchor struct {
	TargetURL     string
	Text          string
	NoFollow      bool
	UserGenerated bool
	Sponsored     bool
}

type SafetyLabels struct {
	RatingValues   []string
	FamilyFriendly *bool
}

type ImageMetadata struct {
	URL     string
	AltText string
}
