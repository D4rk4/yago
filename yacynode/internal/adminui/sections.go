package adminui

func pending(heading, message string) sectionView {
	return sectionView{Heading: heading, Message: message}
}

func defaultSections() map[string]sectionView {
	return map[string]sectionView{
		"/admin/search": pending(
			"Search",
			"Admin search, source toggle, and query explain appear here.",
		),
		"/admin/crawl": pending(
			"Crawler",
			"Crawl start, monitor, results, and profiles appear here.",
		),
		"/admin/network": pending(
			"Network",
			"Peers, seed lists, DHT gates, and transfers appear here.",
		),
		"/admin/index": pending(
			"Index",
			"Index stats, document and term browsing, and blacklists appear here.",
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
