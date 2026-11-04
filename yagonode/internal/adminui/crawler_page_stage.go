package adminui

func crawlerPageStageLabel(stage string) string {
	switch stage {
	case "robots":
		return "checking robots rules"
	case "http_fetch":
		return "fetching page"
	case "html_parse":
		return "parsing page"
	case "frontier_enqueue":
		return "adding discovered URLs"
	case "ingest_queue":
		return "queueing page for indexing"
	case "node_ingest":
		return "waiting for node to store and index"
	default:
		return "processing page"
	}
}
