package recrawlfrontier

import (
	"context"
	"testing"
	"time"
)

func TestSitemapSourceModificationAdvancesInitialRecrawl(t *testing.T) {
	frontier := openTestFrontier(t)
	profile := profileWithRecrawl("Sitemap", 24*time.Hour)
	if err := frontier.RecordProfile(t.Context(), profile); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	if err := frontier.RecordFetchWithSourceModified(
		t.Context(),
		"https://example.org/current",
		profile.Handle,
		testBase,
		testBase.Add(-6*time.Hour),
	); err != nil {
		t.Fatalf("record sitemap fetch: %v", err)
	}
	if due := claim(t, frontier, testBase.Add(17*time.Hour), 1); len(due) != 0 {
		t.Fatalf("claimed before sitemap-adjusted due time: %+v", due)
	}
	due := claim(t, frontier, testBase.Add(18*time.Hour), 1)
	if len(due) != 1 || due[0].URL != "https://example.org/current" {
		t.Fatalf("sitemap-adjusted due = %+v", due)
	}
}

func TestAdvancingSitemapSourceModificationLearnsChangeInterval(t *testing.T) {
	frontier := openTestFrontier(t)
	profile := profileWithRecrawl("Changing sitemap", 24*time.Hour)
	if err := frontier.RecordProfile(t.Context(), profile); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	url := "https://example.org/changing"
	firstSourceModified := testBase.Add(-48 * time.Hour)
	if err := frontier.RecordFetchWithSourceModified(
		t.Context(), url, profile.Handle, testBase, firstSourceModified,
	); err != nil {
		t.Fatalf("record first sitemap fetch: %v", err)
	}
	secondFetch := testBase.Add(24 * time.Hour)
	if err := frontier.RecordFetchWithSourceModified(
		t.Context(),
		url,
		profile.Handle,
		secondFetch,
		firstSourceModified.Add(6*time.Hour),
	); err != nil {
		t.Fatalf("record changed sitemap fetch: %v", err)
	}
	if due := claim(t, frontier, secondFetch.Add(5*time.Hour), 1); len(due) != 0 {
		t.Fatalf("claimed before learned cadence: %+v", due)
	}
	if due := claim(t, frontier, secondFetch.Add(6*time.Hour), 1); len(due) != 1 {
		t.Fatalf("learned sitemap cadence due = %+v", due)
	}
}

func TestFutureSitemapSourceModificationIsIgnored(t *testing.T) {
	frontier := openTestFrontier(t)
	profile := profileWithRecrawl("Future sitemap", 24*time.Hour)
	if err := frontier.RecordProfile(t.Context(), profile); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	if err := frontier.RecordFetchesWithSourceModified(
		t.Context(),
		[]string{"https://example.org/future"},
		[]string{profile.Handle},
		[]time.Time{testBase},
		[]time.Time{testBase.Add(time.Hour)},
	); err != nil {
		t.Fatalf("record future sitemap fetch: %v", err)
	}
	if due := claim(t, frontier, testBase.Add(23*time.Hour), 1); len(due) != 0 {
		t.Fatalf("future signal advanced recrawl: %+v", due)
	}
	if due := claim(t, frontier, testBase.Add(24*time.Hour), 1); len(due) != 1 {
		t.Fatalf("baseline due = %+v", due)
	}
}

func TestSourceModifiedBatchRejectsMismatchedHints(t *testing.T) {
	frontier := openTestFrontier(t)
	err := frontier.RecordFetchesWithSourceModified(
		context.Background(),
		[]string{"https://example.org/a"},
		[]string{"profile"},
		[]time.Time{testBase},
		[]time.Time{},
	)
	if err == nil {
		t.Fatal("mismatched source-modified hints were accepted")
	}
}
