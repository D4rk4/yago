package frontier_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func legacyAutomaticCheckpointPages(
	profileHandle string,
	pageNames ...string,
) []frontiercheckpoint.Page {
	observedAt := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	pages := make([]frontiercheckpoint.Page, 0, len(pageNames))
	for _, pageName := range pageNames {
		host := pageName + ".example"
		pages = append(pages, frontiercheckpoint.Page{
			URL:           "https://" + host + "/page",
			Host:          host,
			ProfileHandle: profileHandle,
			ObservationID: pageName,
			ObservedAt:    observedAt,
			Index:         true,
		})
	}

	return pages
}

type legacyAutomaticCheckpointRun struct {
	provenance     []byte
	identity       []byte
	pages          []frontiercheckpoint.Page
	completedPages int
}

func writeLegacyAutomaticDiscoveryCheckpoint(
	t *testing.T,
	checkpoint *frontiercheckpoint.FrontierCheckpoint,
	run legacyAutomaticCheckpointRun,
) {
	t.Helper()
	if err := checkpoint.Begin(
		context.Background(),
		run.provenance,
		run.identity,
		yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
	); err != nil {
		t.Fatalf("begin legacy automatic checkpoint: %v", err)
	}
	admitted, err := checkpoint.Admit(context.Background(), run.provenance, run.pages)
	if err != nil || admitted != len(run.pages) {
		t.Fatalf("admit legacy checkpoint pages = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(
		context.Background(),
		run.provenance,
		yagocrawlcontract.CrawlRunTally{},
	); err != nil {
		t.Fatalf("finish legacy checkpoint seeding: %v", err)
	}
	for _, page := range run.pages[:run.completedPages] {
		if err := checkpoint.CompletePage(
			context.Background(),
			run.provenance,
			page.URL,
			frontiercheckpoint.PageCompletion{},
		); err != nil {
			t.Fatalf("complete legacy page %q: %v", page.URL, err)
		}
	}
}
