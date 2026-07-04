package adminui

import "html/template"

const overviewWelcome = template.HTML(`
<div class="cds-tile-grid">
  <section class="cds-tile">
    <h2 class="cds-metric__label">Operator console</h2>
    <p>Manage this yago node: monitor crawling and the index, inspect the peer
    network, run admin search, and adjust configuration. Each section fills in as
    it is enabled.</p>
  </section>
  <section class="cds-tile">
    <h2 class="cds-metric__label">Public search</h2>
    <p>The anonymous public search portal is a separate surface and is off by
    default. <a href="/search">Open public search</a>.</p>
  </section>
</div>`)

func pending(heading, message string) sectionView {
	return sectionView{Heading: heading, Message: message}
}

func defaultSections() map[string]sectionView {
	return map[string]sectionView{
		"/admin/overview": {Heading: "Overview", Available: true, Body: overviewWelcome},
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
