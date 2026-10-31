package yagonode

import "github.com/D4rk4/yago/yagonode/internal/adminui"

func crawlOrderImporter(crawl crawlProcess) adminui.CrawlOrderImporter {
	importer, _ := crawl.(adminui.CrawlOrderImporter)
	return importer
}
