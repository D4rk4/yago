package adminui

const recentCrawlHistoryLimit = 10

type crawlRunSections struct {
	Active []CrawlRunView
	Recent []CrawlRunView
}

func buildCrawlRunSections(runs []CrawlRunView) crawlRunSections {
	sections := crawlRunSections{
		Active: make([]CrawlRunView, 0, len(runs)),
		Recent: make([]CrawlRunView, 0, min(len(runs), recentCrawlHistoryLimit)),
	}
	for _, run := range runs {
		if run.State == "running" {
			sections.Active = append(sections.Active, run)
			continue
		}
		if (run.State == "finished" || run.State == "cancelled") &&
			len(sections.Recent) < recentCrawlHistoryLimit {
			sections.Recent = append(sections.Recent, run)
		}
	}

	return sections
}
