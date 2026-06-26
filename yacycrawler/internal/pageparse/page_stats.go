package pageparse

import "github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlwork"

func BuildPageStats(page crawlwork.ParsedPage) crawlwork.PageStats {
	local, external := ResolveLinks(page.URL, page.Links)
	return crawlwork.PageStats{
		Tokens:        Tokenize(page.Text),
		TitleTokens:   Tokenize(page.Title),
		LocalLinks:    local,
		ExternalLinks: external,
	}
}
