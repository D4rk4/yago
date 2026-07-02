package pageparse

type ParsedPage struct {
	URL             string
	CanonicalURL    string
	Description     string
	Title           string
	Headings        []string
	Language        string
	Text            string
	Links           []string
	FollowableLinks []string
	NoFollowLinks   []string
}

type PageStats struct {
	Tokens        []string
	TitleTokens   []string
	LocalLinks    []string
	ExternalLinks []string
}
