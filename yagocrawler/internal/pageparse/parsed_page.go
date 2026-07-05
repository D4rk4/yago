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
