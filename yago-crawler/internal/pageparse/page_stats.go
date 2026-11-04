package pageparse

func BuildPageStats(page ParsedPage) PageStats {
	return buildPageStats(page, 0)
}

func BuildBoundedPageStats(page ParsedPage, maximumLinks int) PageStats {
	return buildPageStats(page, maximumLinks)
}

func buildPageStats(page ParsedPage, maximumLinks int) PageStats {
	local, external := resolveLinks(
		page.URL,
		followableLinks(page),
		maximumLinks,
	)
	return PageStats{
		LocalLinks:    local,
		ExternalLinks: external,
	}
}
