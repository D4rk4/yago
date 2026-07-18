package pageparse

import "time"

type ParsedPage struct {
	URL             string
	CanonicalURL    string
	Description     string
	Author          string
	Keywords        string
	Publisher       string
	PublishedAt     time.Time
	ModifiedAt      time.Time
	DateConfidence  float64
	DateSource      string
	Title           string
	Headings        []string
	Language        string
	Text            string
	Links           []string
	FollowableLinks []string
	NoFollowLinks   []string
	OutboundAnchors []OutboundAnchor
	SafetyLabels    SafetyLabels
	Images          []ImageMetadata
	// MetaNoindex reports a page-level <meta name="robots"> noindex (or none)
	// directive: the page asked not to be indexed.
	MetaNoindex bool
	// MetaNofollow reports a page-level <meta name="robots"> nofollow (or
	// none) directive: the page asked for its links not to be followed.
	MetaNofollow bool
}

type PageStats struct {
	Tokens        []string
	TitleTokens   []string
	LocalLinks    []string
	ExternalLinks []string
}

type ImageMetadata struct {
	URL     string
	AltText string
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
