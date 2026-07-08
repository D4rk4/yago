package pageparse

type ParsedPage struct {
	URL             string
	CanonicalURL    string
	Description     string
	Author          string
	Title           string
	Headings        []string
	Language        string
	Text            string
	Links           []string
	FollowableLinks []string
	NoFollowLinks   []string
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
