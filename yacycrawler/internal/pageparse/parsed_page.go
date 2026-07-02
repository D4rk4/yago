package pageparse

type ParsedPage struct {
	URL          string
	CanonicalURL string
	Description  string
	Title        string
	Headings     []string
	Language     string
	Text         string
	Links        []string
}

type PageStats struct {
	Tokens        []string
	TitleTokens   []string
	LocalLinks    []string
	ExternalLinks []string
}
