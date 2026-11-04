package adminui

import "testing"

func TestCrawlerPageStageLabel(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"robots":           "checking robots rules",
		"http_fetch":       "fetching page",
		"html_parse":       "parsing page",
		"frontier_enqueue": "adding discovered URLs",
		"ingest_queue":     "queueing page for indexing",
		"node_ingest":      "waiting for node to store and index",
		"future-stage":     "processing page",
	}
	for stage, want := range cases {
		if got := crawlerPageStageLabel(stage); got != want {
			t.Errorf("crawlerPageStageLabel(%q) = %q, want %q", stage, got, want)
		}
	}
}
