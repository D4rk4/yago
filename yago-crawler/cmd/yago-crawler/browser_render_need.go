package main

import (
	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

func browserRenderNeeded(page pagefetch.FetchedPage) bool {
	return pageparse.BrowserRenderNeeded(page.ContentType, page.Body)
}
