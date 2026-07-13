package pageparse

func BuildPageStats(page ParsedPage) PageStats {
	return buildPageStats(page, 0, 0, 0)
}

func BuildBoundedPageStats(
	page ParsedPage,
	maximumWords int,
	maximumTitleWords int,
	maximumLinks int,
) PageStats {
	return buildPageStats(page, maximumWords, maximumTitleWords, maximumLinks)
}

func buildPageStats(
	page ParsedPage,
	maximumWords int,
	maximumTitleWords int,
	maximumLinks int,
) PageStats {
	local, external := resolveLinks(
		page.URL,
		followableLinks(page),
		maximumLinks,
	)
	return PageStats{
		Tokens:        tokenize(page.Text, maximumWords),
		TitleTokens:   tokenize(page.Title, maximumTitleWords),
		LocalLinks:    local,
		ExternalLinks: external,
	}
}
