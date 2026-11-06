package yagonode

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const conditionalPageURL = "https://example.org/conditional"

var conditionalBase = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)

type conditionalIngestStream chan crawlresults.IngestDelivery

func (s conditionalIngestStream) Receive() <-chan crawlresults.IngestDelivery { return s }

type conditionalRecrawlNode struct {
	storage    nodeStorage
	ingest     crawlIngestRuntime
	deliveries conditionalIngestStream
	profile    yagocrawlcontract.CrawlProfile
	stop       func()
}

func openConditionalRecrawlNode(
	t *testing.T,
	path string,
	recrawl time.Duration,
) *conditionalRecrawlNode {
	t.Helper()
	storageVault, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open node vault: %v", err)
	}
	storage, err := openNodeStorage(storageVault, "")
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	deliveries := make(conditionalIngestStream)
	ingest, err := openCrawlIngestRuntime(crawlConfig{}, storage, storageVault, deliveries)
	if err != nil {
		t.Fatalf("open crawl ingest runtime: %v", err)
	}
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:            "Conditional",
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
		RecrawlIfOlder:  recrawl,
	})
	if err := ingest.frontier.RecordProfile(
		context.Background(), profile, yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ingest.consumer.Run(ctx)
		close(done)
	}()
	node := &conditionalRecrawlNode{
		storage: storage, ingest: ingest, deliveries: deliveries, profile: profile,
	}
	node.stop = func() {
		cancel()
		<-done
		_ = storageVault.Close()
	}
	t.Cleanup(func() { node.stop() })

	return node
}

func (n *conditionalRecrawlNode) restart(t *testing.T, path string) *conditionalRecrawlNode {
	t.Helper()
	n.stop()
	n.stop = func() {}

	return openConditionalRecrawlNode(t, path, n.profile.RecrawlIfOlder)
}

func (n *conditionalRecrawlNode) submit(t *testing.T, batch yagocrawlcontract.IngestBatch) string {
	t.Helper()
	batch.ProfileHandle = n.profile.Handle
	batch.Provenance = []byte("conditional-run")
	settled := make(chan string, 1)
	report := func(outcome string) func(context.Context) error {
		return func(context.Context) error {
			settled <- outcome

			return nil
		}
	}
	n.deliveries <- crawlresults.IngestDelivery{
		Batch: batch,
		Ack:   report("ack"),
		Nak:   report("nak"),
		Reject: func(ctx context.Context, _ string) error {
			return report("reject")(ctx)
		},
		LeaseLost:              report("lease lost"),
		AckRecordedNotModified: report("recorded"),
	}
	select {
	case outcome := <-settled:
		return outcome
	case <-time.After(5 * time.Second):
		t.Fatal("ingest delivery did not settle")

		return ""
	}
}

func (n *conditionalRecrawlNode) dueRequests(
	t *testing.T,
	now time.Time,
) []yagocrawlcontract.CrawlRequest {
	t.Helper()
	publisher := &capturingPublisher{}
	recrawlSweeper{
		frontier:    n.ingest.frontier,
		publisher:   publisher,
		mint:        func() []byte { return []byte("recrawl") },
		now:         func() time.Time { return now },
		batch:       8,
		conditional: n.ingest.conditional,
	}.sweepOnce(context.Background())
	var requests []yagocrawlcontract.CrawlRequest
	for _, order := range publisher.snapshot() {
		requests = append(requests, order.Requests...)
	}

	return requests
}

type conditionalPageState struct {
	document documentstore.Document
	indexed  int
	postings int
}

func (n *conditionalRecrawlNode) pageState(t *testing.T) conditionalPageState {
	t.Helper()
	ctx := context.Background()
	document, found, err := n.storage.documentDirectory.Document(ctx, conditionalPageURL)
	if err != nil || !found {
		t.Fatalf("stored document = %v, %v", found, err)
	}
	stats, err := n.storage.searchIndex.Stats(ctx)
	if err != nil {
		t.Fatalf("search index stats: %v", err)
	}
	postings, err := n.storage.postings.RWICount(ctx)
	if err != nil {
		t.Fatalf("posting count: %v", err)
	}

	return conditionalPageState{document: document, indexed: stats.Documents, postings: postings}
}

func fullConditionalPage(
	t *testing.T,
	observedAt time.Time,
	text string,
	validators *yagocrawlcontract.HTTPValidators,
) yagocrawlcontract.IngestBatch {
	t.Helper()
	urlHash, err := yagomodel.HashURL(conditionalPageURL)
	if err != nil {
		t.Fatalf("hash page url: %v", err)
	}

	return yagocrawlcontract.IngestBatch{
		SourceURL:          conditionalPageURL,
		ObservationID:      "full-" + observedAt.Format(time.RFC3339Nano),
		ObservedAt:         observedAt,
		ResponseValidators: validators,
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL:        conditionalPageURL,
			CanonicalURL:         conditionalPageURL,
			Title:                "Synthetic conditional page",
			ExtractedText:        text,
			ContentType:          "text/html",
			ContentHash:          "sha256:" + text,
			ExtractionGeneration: yagocrawlcontract.CurrentExtractionGeneration,
			FetchedAt:            observedAt,
		},
		Postings: []yagomodel.RWIPosting{{
			WordHash:   yagomodel.WordHash("conditional"),
			Properties: map[string]string{yagomodel.ColURLHash: urlHash.String()},
		}},
	}
}

func notModifiedConditionalPage(
	revision yagocrawlcontract.DocumentRevision,
	observedAt time.Time,
	observationID string,
) yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{
		SourceURL:     conditionalPageURL,
		ObservationID: observationID,
		ObservedAt:    observedAt,
		NotModified:   &yagocrawlcontract.NotModifiedObservation{Revision: revision},
	}
}

func firstValidators() *yagocrawlcontract.HTTPValidators {
	return &yagocrawlcontract.HTTPValidators{ETag: `"first"`}
}

func soleConditionalFetch(
	t *testing.T,
	requests []yagocrawlcontract.CrawlRequest,
) *yagocrawlcontract.ConditionalFetch {
	t.Helper()
	if len(requests) != 1 || requests[0].URL != conditionalPageURL {
		t.Fatalf("due requests = %+v, want the conditional page", requests)
	}

	return requests[0].ConditionalFetch
}

func TestConditionalRecrawlConfirmsAnUnchangedPageWithoutRewritingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	node := openConditionalRecrawlNode(t, path, time.Hour)
	if outcome := node.submit(
		t, fullConditionalPage(t, conditionalBase, "first text", firstValidators()),
	); outcome != "ack" {
		t.Fatalf("full page settlement = %q, want ack", outcome)
	}
	if requests := node.dueRequests(t, conditionalBase.Add(59*time.Minute)); len(requests) != 0 {
		t.Fatalf("requests before the page is due = %+v", requests)
	}
	fetch := soleConditionalFetch(t, node.dueRequests(t, conditionalBase.Add(time.Hour)))
	if fetch == nil || fetch.Validators != *firstValidators() ||
		fetch.Revision.ContentHash != "sha256:first text" ||
		!fetch.Revision.FetchedAt.Equal(conditionalBase) {
		t.Fatalf("issued conditional fetch = %+v", fetch)
	}
	before := node.pageState(t)
	if before.indexed != 1 || before.postings != 1 {
		t.Fatalf("stored page state = %+v, want one indexed document and one posting", before)
	}
	observedAt := conditionalBase.Add(61 * time.Minute)

	for _, observationID := range []string{"unchanged-first", "unchanged-replayed"} {
		outcome := node.submit(
			t, notModifiedConditionalPage(fetch.Revision, observedAt, observationID),
		)
		if outcome != "recorded" {
			t.Fatalf("%s settlement = %q, want recorded", observationID, outcome)
		}
	}

	if after := node.pageState(t); !reflect.DeepEqual(after, before) {
		t.Fatalf("not-modified changed the page:\n got %+v\nwant %+v", after, before)
	}
	if requests := node.dueRequests(t, conditionalBase.Add(2*time.Hour)); len(requests) != 0 {
		t.Fatalf("requests before the advanced due time = %+v", requests)
	}
	node = node.restart(t, path)
	again := soleConditionalFetch(t, node.dueRequests(t, observedAt.Add(time.Hour)))
	if again == nil || !reflect.DeepEqual(*again, *fetch) {
		t.Fatalf("conditional fetch after restart = %+v, want %+v", again, fetch)
	}
	if outcome := node.submit(
		t, notModifiedConditionalPage(fetch.Revision, observedAt, "unchanged-after-restart"),
	); outcome != "recorded" {
		t.Fatalf("replay after restart settlement = %q, want recorded", outcome)
	}
}
