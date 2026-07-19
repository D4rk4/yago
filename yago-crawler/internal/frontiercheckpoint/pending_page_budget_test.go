package frontiercheckpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestHistoricalCheckpointLoadsWithoutBudgetDiscardAccounting(t *testing.T) {
	checkpoint, provenance, _ := admittedCheckpoint(t)
	if err := checkpoint.readTransaction(context.Background(), func(transaction *bolt.Tx) error {
		encoded := transaction.Bucket(runsBucket).Get(provenance)
		if bytes.Contains(encoded, []byte("budget_discarded_pages")) {
			t.Fatalf("zero budget discard field was persisted: %s", encoded)
		}

		return nil
	}); err != nil {
		t.Fatalf("read pre-field run record: %v", err)
	}
	snapshot, err := checkpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load pre-field run record: %v", err)
	}
	if snapshot.BudgetDiscardedPages != 0 {
		t.Fatalf("pre-field budget discarded pages = %d, want 0", snapshot.BudgetDiscardedPages)
	}
}

func TestTrimPendingPagesKeepsOldestOutstandingPages(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("pending-page-budget")
	beginTestRun(t, checkpoint, provenance, []byte("pending-page-budget-order"))
	pages := []Page{
		testPage("https://one.example/page", "one.example", "one", 0),
		testPage("https://two.example/page", "two.example", "two", 0),
		testPage("https://three.example/page", "three.example", "three", 0),
	}
	admitted, err := checkpoint.Admit(context.Background(), provenance, pages)
	if err != nil || admitted != len(pages) {
		t.Fatalf("admit pending pages = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(
		context.Background(),
		provenance,
		yagocrawlcontract.CrawlRunTally{},
	); err != nil {
		t.Fatalf("finish pending page seeding: %v", err)
	}
	removed, err := checkpoint.TrimPendingPages(context.Background(), provenance, 2)
	if err != nil || removed != 1 {
		t.Fatalf("trim pending pages = %d, %v", removed, err)
	}
	snapshot, err := checkpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load trimmed pages: %v", err)
	}
	if snapshot.Counters.Pending != 2 || len(snapshot.Outstanding) != 2 ||
		snapshot.BudgetDiscardedPages != 1 ||
		snapshot.Counters.Pages != 3 || len(snapshot.Visited) != 3 ||
		snapshot.HostStates["one.example"].Pages != 1 ||
		snapshot.HostStates["two.example"].Pages != 1 ||
		snapshot.HostStates["three.example"].Pages != 1 ||
		snapshot.Outstanding[0].URL != pages[0].URL || snapshot.Outstanding[1].URL != pages[1].URL {
		t.Fatalf("trimmed snapshot = %+v", snapshot)
	}
	removed, err = checkpoint.TrimPendingPages(context.Background(), provenance, 2)
	if err != nil || removed != 0 {
		t.Fatalf("repeat trim = %d, %v", removed, err)
	}
}

func TestTrimPendingPagesRejectsCorruptBudgetAccounting(t *testing.T) {
	checkpoint, provenance, _ := admittedCheckpoint(t)
	mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
		record.BudgetDiscardedPages = 1
	})
	if _, err := checkpoint.TrimPendingPages(context.Background(), provenance, 1); err == nil {
		t.Fatal("expected corrupt page budget accounting error")
	}
}

func TestTrimPendingPagesRejectsInvalidOrMissingRun(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	if _, err := checkpoint.TrimPendingPages(context.Background(), nil, 0); !errors.Is(
		err,
		ErrInvalidProvenance,
	) {
		t.Fatalf("invalid provenance error = %v", err)
	}
	if _, err := checkpoint.TrimPendingPages(
		context.Background(),
		[]byte("missing-page-budget-run"),
		0,
	); !errors.Is(
		err,
		ErrRunNotFound,
	) {
		t.Fatalf("missing run error = %v", err)
	}
}

func TestTrimPendingPagesRejectsMissingPageBucket(t *testing.T) {
	checkpoint, provenance, _ := admittedCheckpoint(t)
	deleteSchemaBucket(t, checkpoint, pagesBucket)
	if _, err := checkpoint.TrimPendingPages(
		context.Background(),
		provenance,
		0,
	); !errors.Is(
		err,
		ErrCorruptCheckpoint,
	) {
		t.Fatalf("missing page bucket error = %v", err)
	}
}

func TestTrimPendingPagesRejectsCorruptOutstandingPages(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*bolt.Tx, []byte, Page) error
	}{
		{name: "key", mutate: putBadPageKey},
		{name: "encoding", mutate: putBadPageEncoding},
		{name: "page fields", mutate: putBadPageFields},
		{name: "position", mutate: putBadPagePosition},
		{
			name: "pending rows",
			mutate: func(transaction *bolt.Tx, _ []byte, _ Page) error {
				return writeRunRecord(transaction, []byte("corrupt-run"), runRecord{
					OrderIdentity: []byte("corrupt-identity"),
					Seeding:       true,
					Pages:         2,
					Pending:       2,
				})
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			prefix, err := provenancePrefix(provenance)
			if err != nil {
				t.Fatalf("page budget prefix: %v", err)
			}
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, prefix, page)
			})
			if _, err := checkpoint.TrimPendingPages(
				context.Background(),
				provenance,
				0,
			); !errors.Is(
				err,
				ErrCorruptCheckpoint,
			) {
				t.Fatalf("corrupt outstanding page error = %v", err)
			}
		})
	}
}

func TestNewestPendingPageURLsIgnoresNonpositiveLimit(t *testing.T) {
	checkpoint, provenance, _ := admittedCheckpoint(t)
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		t.Fatalf("page budget prefix: %v", err)
	}
	if err := checkpoint.readTransaction(context.Background(), func(transaction *bolt.Tx) error {
		pages, err := schemaBucket(transaction, pagesBucket)
		if err != nil {
			return err
		}
		pageURLs, err := newestPendingPageURLs(pages, prefix, 0)
		if err != nil || pageURLs != nil {
			t.Fatalf("nonpositive page lookup = %v, %v", pageURLs, err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read page budget checkpoint: %v", err)
	}
}

func TestPendingPageBudgetResumesAfterCommittedBatch(t *testing.T) {
	const pageTotal uint64 = pendingPageBudgetBatchSize + 2

	path := testCheckpointPath(t)
	first := openTestCheckpoint(t, path)
	provenance := []byte("batched-page-budget")
	beginTestRun(t, first, provenance, []byte("batched-page-budget-order"))
	pages := make([]Page, 0, pendingPageBudgetBatchSize+2)
	for index := range pendingPageBudgetBatchSize + 2 {
		pages = append(pages, testPage(
			fmt.Sprintf("https://example.com/page/%03d", index),
			"example.com",
			fmt.Sprintf("observation-%03d", index),
			0,
		))
	}
	admitted, err := first.Admit(context.Background(), provenance, pages)
	if err != nil || admitted != len(pages) {
		t.Fatalf("admit batched pending pages = %d, %v", admitted, err)
	}
	if err := first.FinishSeeding(
		context.Background(),
		provenance,
		yagocrawlcontract.CrawlRunTally{},
	); err != nil {
		t.Fatalf("finish batched pending page seeding: %v", err)
	}
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		t.Fatalf("page budget prefix: %v", err)
	}
	removed, complete, err := first.trimPendingPageBudgetBatch(
		context.Background(),
		provenance,
		prefix,
		1,
	)
	if err != nil || removed != pendingPageBudgetBatchSize || complete {
		t.Fatalf("first page budget batch = %d/%v, %v", removed, complete, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first page budget checkpoint: %v", err)
	}

	second := openTestCheckpoint(t, path)
	removed, err = second.TrimPendingPages(context.Background(), provenance, 1)
	if err != nil || removed != 1 {
		t.Fatalf("resumed page budget trim = %d, %v", removed, err)
	}
	snapshot, err := second.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load resumed page budget: %v", err)
	}
	if snapshot.Counters.Pages != pageTotal || snapshot.Counters.Pending != 1 ||
		snapshot.BudgetDiscardedPages != pageTotal-1 ||
		len(snapshot.Visited) != len(pages) ||
		snapshot.HostStates["example.com"].Pages != pageTotal ||
		len(snapshot.Outstanding) != 1 || snapshot.Outstanding[0].URL != pages[0].URL {
		t.Fatalf("resumed page budget snapshot = %+v", snapshot)
	}
}
