package frontier

import (
	"context"
	"testing"
	"time"

	bbolt "go.etcd.io/bbolt"
)

func TestFetchedPageOutcomeTallySeparatesMutationAndNoMutation(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	accepted := settleOutcomePage(t, frontier, ctx, "accepted", PageOutcome{
		Class: OutcomeDocumentCandidate,
	}, true)
	settleOutcomePage(t, frontier, ctx, "gone", PageOutcome{
		Class: OutcomeRemovalMutation,
	}, true)
	rejected := settleOutcomePage(t, frontier, ctx, "rejected", PageOutcome{
		Class: OutcomeDocumentCandidate,
	}, false)
	if err := frontier.CompletePendingIngestWithOutcome(
		ctx, rejected.ID, false, "too-few-words",
	); err != nil {
		t.Fatalf("complete rejected ingest: %v", err)
	}
	noMutation, _, err := frontier.Enqueue(
		ctx, "run-outcomes", "https://three.example/sitemap.xml", "", 0, time.Time{},
	)
	if err != nil {
		t.Fatalf("enqueue sitemap: %v", err)
	}
	if _, found, err := frontier.Claim(ctx, noMutation.RunID, time.Now()); err != nil || !found {
		t.Fatalf("claim sitemap = %v, err %v", found, err)
	}
	if err := frontier.SettleWithOutcome(ctx, noMutation.ID, StateFetched, time.Time{}, PageOutcome{
		Class: OutcomeNotIndexable, Reason: OutcomeSitemap,
	}); err != nil {
		t.Fatalf("settle sitemap: %v", err)
	}
	depth, err := frontier.DepthForRun(ctx, "run-outcomes")
	if err != nil {
		t.Fatalf("read outcome tally: %v", err)
	}
	if depth.Fetched != 2 || depth.Indexed != 2 || depth.DocumentCandidates != 2 ||
		depth.RemovalMutations != 1 ||
		depth.NodeRejected != 1 || depth.NotIndexable != 1 {
		t.Fatalf("outcome tally = %+v", depth)
	}
	if len(depth.OutcomeCounts) != 2 || depth.OutcomeCounts[0].Class != OutcomeNodeRejected ||
		depth.OutcomeCounts[0].Reason != "too-few-words" || depth.OutcomeCounts[0].Pages != 1 ||
		depth.OutcomeCounts[1].Class != OutcomeNotIndexable ||
		depth.OutcomeCounts[1].Reason != OutcomeSitemap || depth.OutcomeCounts[1].Pages != 1 {
		t.Fatalf("reason tally = %+v", depth.OutcomeCounts)
	}
	var acceptedPage Page
	if err := frontier.database.View(func(transaction *bolt.Tx) error {
		var err error
		acceptedPage, err = loadPage(transaction.Bucket([]byte(pagesBucketName)).Get([]byte(accepted.ID)))
		return err
	}); err != nil {
		t.Fatalf("read accepted page: %v", err)
	}
	if acceptedPage.State != StateIndexed || acceptedPage.Outcome != OutcomeDocumentCandidate {
		t.Fatalf("accepted page state = %+v", acceptedPage)
	}
}

func TestSettleRejectsUnknownNotIndexableReasonWithoutChangingPage(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	page, _, err := frontier.Enqueue(
		ctx, "run-validation", "https://one.example/page", "", 0, time.Time{},
	)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, found, err := frontier.Claim(ctx, page.RunID, time.Now().Add(2*time.Second)); err != nil || !found {
		t.Fatalf("claim = %v, err %v", found, err)
	}
	if err := frontier.SettleWithOutcome(ctx, page.ID, StateFetched, time.Time{}, PageOutcome{
		Class: OutcomeNotIndexable, Reason: "unbounded-value",
	}); err == nil {
		t.Fatal("unknown outcome reason was accepted")
	}
	if err := frontier.SettleWithOutcome(ctx, page.ID, StateFetched, time.Time{}, PageOutcome{
		Class: OutcomeNotIndexable, Reason: OutcomeEmptyContent,
	}); err != nil {
		t.Fatalf("valid outcome reason was refused: %v", err)
	}
	depth, err := frontier.DepthForRun(ctx, page.RunID)
	if err != nil || depth.NotIndexable != 1 || len(depth.OutcomeCounts) != 1 ||
		depth.OutcomeCounts[0].Reason != OutcomeEmptyContent {
		t.Fatalf("valid outcome tally = %+v, err %v", depth, err)
	}
}

func settleOutcomePage(
	t *testing.T,
	frontier *Frontier,
	ctx context.Context,
	path string,
	outcome PageOutcome,
	accepted bool,
) Page {
	t.Helper()
	page, _, err := frontier.Enqueue(
		ctx, "run-outcomes", "https://one.example/"+path, "", 0, time.Time{},
	)
	if err != nil {
		t.Fatalf("enqueue %s: %v", path, err)
	}
	if _, found, err := frontier.Claim(ctx, page.RunID, time.Now().Add(2*time.Second)); err != nil || !found {
		t.Fatalf("claim %s = %v, err %v", path, found, err)
	}
	if err := frontier.StorePendingIngest(ctx, page.ID, "lease", []byte(`{"batch":true}`)); err != nil {
		t.Fatalf("store pending ingest %s: %v", path, err)
	}
	if err := frontier.SettleWithOutcome(ctx, page.ID, StateFetched, time.Time{}, outcome); err != nil {
		t.Fatalf("settle %s: %v", path, err)
	}
	if accepted {
		if err := frontier.CompletePendingIngest(ctx, page.ID, true); err != nil {
			t.Fatalf("complete accepted ingest %s: %v", path, err)
		}
	}
	return page
}
