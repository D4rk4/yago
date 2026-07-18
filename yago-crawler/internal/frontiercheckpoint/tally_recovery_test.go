package frontiercheckpoint

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestPageTallyCommitsWithItsOutstandingRemoval(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	provenance := []byte("page-tally-atomicity")
	identity := []byte("page-tally-identity")
	beginTestRun(t, checkpoint, provenance, identity)
	first := testPage("https://example.com/first", "example.com", "first-observation", 0)
	second := testPage("https://example.com/second", "example.com", "second-observation", 1)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{first, second},
	); err != nil || admitted != 2 {
		t.Fatalf("admit pages = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(
		testContext,
		provenance,
		yagocrawlcontract.CrawlRunTally{Duplicates: 1},
	); err != nil {
		t.Fatalf("finish seeding: %v", err)
	}
	if err := checkpoint.CompletePage(
		testContext,
		provenance,
		first.URL,
		PageCompletion{Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1}},
	); err != nil {
		t.Fatalf("complete first page: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint: %v", err)
	}

	reopened := openTestCheckpoint(t, path)
	snapshot, err := reopened.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load recovered tally: %v", err)
	}
	if snapshot.Tally != (yagocrawlcontract.CrawlRunTally{
		Fetched: 1, Indexed: 1, Duplicates: 1,
	}) {
		t.Fatalf("recovered tally = %+v", snapshot.Tally)
	}
	if len(snapshot.Outstanding) != 1 || snapshot.Outstanding[0].URL != second.URL {
		t.Fatalf("recovered outstanding = %+v, want only second page", snapshot.Outstanding)
	}
	if err := reopened.CompletePage(
		testContext,
		provenance,
		first.URL,
		PageCompletion{Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1}},
	); err != nil {
		t.Fatalf("repeat completed page: %v", err)
	}
	if err := reopened.CompletePage(
		testContext,
		provenance,
		second.URL,
		PageCompletion{Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1, Failed: 1}},
	); err != nil {
		t.Fatalf("complete recovered page: %v", err)
	}
	completed, err := reopened.Load(testContext, provenance)
	if err != nil {
		t.Fatalf("load completed tally: %v", err)
	}
	want := yagocrawlcontract.CrawlRunTally{
		Fetched: 2, Indexed: 1, Failed: 1, Duplicates: 1,
	}
	if completed.Tally != want || !completed.Completed || len(completed.Outstanding) != 0 {
		t.Fatalf("completed snapshot = %+v, want tally %+v", completed, want)
	}
}
