package pageparse

func BuildPageStats(page ParsedPage) PageStats {
	local, external := ResolveLinks(page.URL, page.Links)
	return PageStats{
		Tokens:        Tokenize(page.Text),
		TitleTokens:   Tokenize(page.Title),
		LocalLinks:    local,
		ExternalLinks: external,
	}
}
