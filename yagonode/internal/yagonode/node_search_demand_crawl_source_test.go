package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/searchdemand"
)

func TestSearchDemandCrawlSourceRecordsCanonicalFocusedSeeds(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	demand, err := searchdemand.Open(storage)
	if err != nil {
		t.Fatalf("searchdemand.Open: %v", err)
	}
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	source := newSearchDemandCrawlSource(demand).(searchDemandCrawlSource)
	source.now = func() time.Time { return at }
	result, err := source.Start(context.Background(), adminui.CrawlStart{
		Name: "  Focused   docs ",
		Seeds: []string{
			"https://docs.example.test/guide",
			"https://docs.example.test/guide#section",
		},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if result.Seeds != 1 || result.ProfileHandle != "Focused docs" {
		t.Fatalf("focused demand result = %+v", result)
	}
	page, err := demand.ReadPage(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(page.Records) != 1 || page.Records[0].URL != "https://docs.example.test/guide" ||
		page.Records[0].Topic != "Focused docs" || !page.Records[0].FocusedAt.Equal(at) {
		t.Fatalf("focused demand records = %+v", page.Records)
	}
}

func TestSearchDemandCrawlSourceRejectsInvalidSeedsWithoutPartialWrites(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	demand, err := searchdemand.Open(storage)
	if err != nil {
		t.Fatalf("searchdemand.Open: %v", err)
	}
	source := newSearchDemandCrawlSource(demand)
	if _, err := source.Start(context.Background(), adminui.CrawlStart{
		Name:  "Invalid request",
		Seeds: []string{"https://docs.example.test/", "file:///etc/passwd"},
	}); err == nil {
		t.Fatal("invalid focused seed request was accepted")
	}
	page, err := demand.ReadPage(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("invalid request recorded %d seeds", len(page.Records))
	}
}
