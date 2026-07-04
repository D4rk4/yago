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
		"/admin/performance": pending(
			"Performance",
			"Queues, throughput, and operational controls appear here.",
		),
		"/admin/configuration": pending(
			"Configuration",
			"Identity, storage, proxy, search, and security settings appear here.",
		),
		"/admin/security": pending(
			"Security",
			"Authentication, API keys, and privacy modes appear here.",
		),
		"/admin/logs": pending("Logs", "Structured events and log streaming appear here."),
	}
}
