package adminui

import (
	"testing"
	"time"
)

func TestCrawlerPulseIncludesActiveURLStageAndElapsedTime(t *testing.T) {
	view := buildCrawlerPulseView(CrawlerPulse{
		Available: true,
		Workers: []CrawlerWorkerPulse{{
			WorkerID: "crawler-a",
			ActivePages: []CrawlerActivePage{{
				URL:                 "https://example.org/page",
				Stage:               "node_ingest",
				ElapsedMilliseconds: 6200,
			}},
		}},
	}, time.Now())
	if len(view.WorkerRows) != 1 || len(view.WorkerRows[0].ActivePages) != 1 {
		t.Fatalf("crawler active page view = %#v", view.WorkerRows)
	}
	page := view.WorkerRows[0].ActivePages[0]
	if page.URL != "https://example.org/page" ||
		page.Stage != "waiting for node to store and index" || page.Elapsed != "6s" {
		t.Fatalf("crawler active page = %#v", page)
	}
}
