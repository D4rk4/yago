package yagonode

import (
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlprofilelibrary"
	"github.com/D4rk4/yago/yagonode/internal/events"
)

func applySavedCrawlProfileAdminOptions(
	options *adminui.Options,
	assembled node,
	recorder *events.Recorder,
) {
	dispatcher := crawlDispatcher(assembled.crawl)
	if dispatcher == nil || assembled.vault == nil {
		return
	}
	library, err := crawlprofilelibrary.Open(assembled.vault)
	if err != nil {
		if recorder != nil {
			recorder.Record(
				events.SeverityWarn,
				events.CategoryCrawl,
				"crawl.profile.unavailable",
				"saved crawl profile library unavailable",
			)
		}

		return
	}
	options.SavedCrawlProfiles = newSavedCrawlProfileSource(library, dispatcher, recorder)
}
