package frontiercheckpoint

import (
	"context"
	"errors"
	"fmt"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestDiscardPendingPagesBeyondDepthPreservesAllowedPages(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("pending-page-depth")
	beginTestRun(t, checkpoint, provenance, []byte("pending-page-depth-order"))
	pages := []Page{
		testPage("https://example.com/zero", "example.com", "zero", 0),
		testPage("https://example.com/two", "example.com", "two", 2),
		testPage("https://example.com/one", "example.com", "one", 1),
		testPage("https://example.com/three", "example.com", "three", 3),
	}
	admitted, err := checkpoint.Admit(context.Background(), provenance, pages)
	if err != nil || admitted != len(pages) {
		t.Fatalf("admit depth pages = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(
		context.Background(),
		provenance,
		yagocrawlcontract.CrawlRunTally{},
	); err != nil {
		t.Fatalf("finish depth page seeding: %v", err)
	}
	discarded, err := checkpoint.DiscardPendingPagesBeyondDepth(
		context.Background(),
		provenance,
		1,
	)
	if err != nil || discarded != 2 {
		t.Fatalf("discard deep pages = %d, %v", discarded, err)
	}
	snapshot, err := checkpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load depth-limited pages: %v", err)
	}
	if snapshot.Counters.Pages != 4 || snapshot.Counters.Pending != 2 ||
		snapshot.BudgetDiscardedPages != 2 || len(snapshot.Visited) != 4 ||
		len(snapshot.Outstanding) != 2 || snapshot.Outstanding[0].URL != pages[0].URL ||
		snapshot.Outstanding[1].URL != pages[2].URL ||
		snapshot.HostStates["example.com"].Pages != 4 {
		t.Fatalf("depth-limited snapshot = %+v", snapshot)
	}
	discarded, err = checkpoint.DiscardPendingPagesBeyondDepth(
		context.Background(),
		provenance,
		1,
	)
	if err != nil || discarded != 0 {
		t.Fatalf("repeat depth discard = %d, %v", discarded, err)
	}
}

func TestDiscardPendingPagesBeyondDepthCompletesDrainedRun(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("drained-page-depth")
	beginTestRun(t, checkpoint, provenance, []byte("drained-page-depth-order"))
	page := testPage("https://example.com/deep", "example.com", "deep", 1)
	if admitted, err := checkpoint.Admit(
		context.Background(),
		provenance,
		[]Page{page},
	); err != nil || admitted != 1 {
		t.Fatalf("admit drained depth page = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(
		context.Background(),
		provenance,
		yagocrawlcontract.CrawlRunTally{},
	); err != nil {
		t.Fatalf("finish drained depth seeding: %v", err)
	}
	if discarded, err := checkpoint.DiscardPendingPagesBeyondDepth(
		context.Background(),
		provenance,
		0,
	); err != nil || discarded != 1 {
		t.Fatalf("discard drained depth page = %d, %v", discarded, err)
	}
	state, err := checkpoint.Inspect(
		context.Background(),
		provenance,
		[]byte("drained-page-depth-order"),
	)
	if err != nil || state.Status != RunCompleted {
		t.Fatalf("drained depth state = %+v, %v", state, err)
	}
	if discarded, err := checkpoint.DiscardPendingPagesBeyondDepth(
		context.Background(),
		provenance,
		0,
	); err != nil || discarded != 0 {
		t.Fatalf("repeat drained depth discard = %d, %v", discarded, err)
	}
}

func TestDiscardPendingPagesBeyondDepthRejectsInvalidInputAndState(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	if _, err := checkpoint.DiscardPendingPagesBeyondDepth(
		context.Background(),
		[]byte("depth"),
		-1,
	); err == nil {
		t.Fatal("negative pending page depth accepted")
	}
	if _, err := checkpoint.DiscardPendingPagesBeyondDepth(
		context.Background(),
		nil,
		0,
	); !errors.Is(err, ErrInvalidProvenance) {
		t.Fatalf("invalid depth provenance error = %v", err)
	}
	if _, err := checkpoint.DiscardPendingPagesBeyondDepth(
		context.Background(),
		[]byte("missing-depth-run"),
		0,
	); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing depth run error = %v", err)
	}
}

func TestDiscardPendingPagesBeyondDepthRejectsCorruptCheckpoint(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, *FrontierCheckpoint, []byte, Page)
	}{
		{name: "budget accounting", mutate: corruptPendingDepthBudgetAccounting},
		{name: "pages bucket", mutate: corruptPendingDepthPagesBucket},
		{name: "pending total", mutate: corruptPendingDepthTotal},
		{name: "page key", mutate: corruptPendingDepthPageKey},
		{name: "page encoding", mutate: corruptPendingDepthPageEncoding},
		{name: "page fields", mutate: corruptPendingDepthPageFields},
		{name: "page position", mutate: corruptPendingDepthPagePosition},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			page.Depth = 1
			persistPendingDepthPage(t, checkpoint, provenance, page)
			testCase.mutate(t, checkpoint, provenance, page)
			if _, err := checkpoint.DiscardPendingPagesBeyondDepth(
				context.Background(),
				provenance,
				0,
			); !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("corrupt depth checkpoint error = %v", err)
			}
		})
	}
}

func persistPendingDepthPage(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	page Page,
) {
	t.Helper()
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		t.Fatalf("persisted depth page prefix: %v", err)
	}
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		encoded, err := encodeRow("page", page)
		if err != nil {
			return err
		}

		return putRow(
			transaction.Bucket(pagesBucket),
			sequenceRowKey(prefix, 1),
			encoded,
			"deep test page",
		)
	})
}

func corruptPendingDepthBudgetAccounting(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	_ Page,
) {
	t.Helper()
	mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
		record.BudgetDiscardedPages = 1
	})
}

func corruptPendingDepthPagesBucket(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	_ []byte,
	_ Page,
) {
	t.Helper()
	deleteSchemaBucket(t, checkpoint, pagesBucket)
}

func corruptPendingDepthTotal(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	_ Page,
) {
	t.Helper()
	mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
		record.Pending = 0
	})
}

func mutatePendingDepthPage(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	page Page,
	mutate func(*bolt.Tx, []byte, Page) error,
) {
	t.Helper()
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		t.Fatalf("depth page prefix: %v", err)
	}
	mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
		return mutate(transaction, prefix, page)
	})
}

func corruptPendingDepthPageKey(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	page Page,
) {
	t.Helper()
	mutatePendingDepthPage(t, checkpoint, provenance, page, putBadPageKey)
}

func corruptPendingDepthPageEncoding(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	page Page,
) {
	t.Helper()
	mutatePendingDepthPage(t, checkpoint, provenance, page, putBadPageEncoding)
}

func corruptPendingDepthPageFields(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	page Page,
) {
	t.Helper()
	mutatePendingDepthPage(t, checkpoint, provenance, page, putBadPageFields)
}

func corruptPendingDepthPagePosition(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	provenance []byte,
	page Page,
) {
	t.Helper()
	mutatePendingDepthPage(t, checkpoint, provenance, page, putBadPagePosition)
}

func TestDiscardPendingPagesBeyondDepthResumesAfterBatch(t *testing.T) {
	path := testCheckpointPath(t)
	first := openTestCheckpoint(t, path)
	provenance := []byte("batched-page-depth")
	beginTestRun(t, first, provenance, []byte("batched-page-depth-order"))
	pages := make([]Page, 0, pendingPageBudgetBatchSize+2)
	for index := range pendingPageBudgetBatchSize + 2 {
		pages = append(pages, testPage(
			fmt.Sprintf("https://example.com/page/%03d", index),
			"example.com",
			fmt.Sprintf("depth-observation-%03d", index),
			2,
		))
	}
	admitted, err := first.Admit(context.Background(), provenance, pages)
	if err != nil || admitted != len(pages) {
		t.Fatalf("admit batched depth pages = %d, %v", admitted, err)
	}
	if err := first.FinishSeeding(
		context.Background(),
		provenance,
		yagocrawlcontract.CrawlRunTally{},
	); err != nil {
		t.Fatalf("finish batched depth seeding: %v", err)
	}
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		t.Fatalf("batched depth prefix: %v", err)
	}
	discarded, complete, err := first.discardPendingPageDepthBatch(
		context.Background(),
		provenance,
		prefix,
		1,
	)
	if err != nil || discarded != pendingPageBudgetBatchSize || complete {
		t.Fatalf("first pending depth batch = %d/%v, %v", discarded, complete, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first depth checkpoint: %v", err)
	}
	second := openTestCheckpoint(t, path)
	discarded, err = second.DiscardPendingPagesBeyondDepth(
		context.Background(),
		provenance,
		1,
	)
	if err != nil || discarded != 2 {
		t.Fatalf("resume pending depth discard = %d, %v", discarded, err)
	}
	snapshot, err := second.Load(context.Background(), provenance)
	if err != nil || snapshot.Counters.Pending != 0 ||
		snapshot.BudgetDiscardedPages != uint64(len(pages)) || !snapshot.Completed {
		t.Fatalf("resumed depth snapshot = %+v, %v", snapshot, err)
	}
}
