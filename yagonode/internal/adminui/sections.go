package adminui

func pending(heading, message string) sectionView {
	return sectionView{Heading: heading, Message: message}
}

func defaultSections() map[string]sectionView {
	return map[string]sectionView{
		"/admin/crawl": pending(
			"Crawler",
			"Crawl start, monitor, results, and profiles appear here.",
		),
		"/admin/configuration": pending(
			"Configuration",
			"Identity, storage, proxy, search, and security settings appear here.",
		),
	}
}
