package crawlorder_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlorder"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yago-crawler/internal/runtally"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type rejectingRecoveryExpander struct {
	called chan struct{}
}

type historicalCrawlOrder struct {
	Provenance []byte
	Profile    historicalCrawlProfile
	Requests   []historicalCrawlRequest
}

type historicalCrawlProfile struct {
	Handle          string
	Name            string
	Scope           yagocrawlcontract.CrawlScope
	URLMustMatch    string
	MaxDepth        int
	MaxPagesPerHost int
}

type historicalCrawlRequest struct {
	URL           string
	ProfileHandle string
}

func (e rejectingRecoveryExpander) Expand(
	context.Context,
	[]yagocrawlcontract.CrawlRequest,
) ([]yagocrawlcontract.CrawlRequest, error) {
	close(e.called)

	return nil, context.Canceled
}

func checkpointOrder(t *testing.T, provenance string) (yagocrawlcontract.CrawlOrder, []byte) {
	t.Helper()
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte(provenance),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:           "https://example.com/restored",
			ProfileHandle: profile.Handle,
		}},
	}
	encoded, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	identity := sha256.Sum256(encoded)

	return order, identity[:]
}

func historicalCheckpointOrder(
	t *testing.T,
	provenance string,
) (yagocrawlcontract.CrawlOrder, []byte) {
	t.Helper()
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:            "historical",
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        0,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	payload, err := json.Marshal(historicalCrawlOrder{
		Provenance: []byte(provenance),
		Profile: historicalCrawlProfile{
			Handle:          profile.Handle,
			Name:            profile.Name,
			Scope:           profile.Scope,
			URLMustMatch:    profile.URLMustMatch,
			MaxDepth:        profile.MaxDepth,
			MaxPagesPerHost: profile.MaxPagesPerHost,
		},
		Requests: []historicalCrawlRequest{{
			URL:           "https://example.com/restored",
			ProfileHandle: profile.Handle,
		}},
	})
	if err != nil {
		t.Fatalf("marshal historical order: %v", err)
	}
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(payload)
	if err != nil {
		t.Fatalf("decode historical order: %v", err)
	}
	reencoded, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("re-encode historical order: %v", err)
	}
	identity := sha256.Sum256(payload)
	if identity == sha256.Sum256(reencoded) {
		t.Fatal("historical payload unexpectedly matched the current encoded form")
	}

	return order, identity[:]
}

func openConsumerCheckpoint(t *testing.T) *frontiercheckpoint.FrontierCheckpoint {
	t.Helper()
	checkpoint, err := frontiercheckpoint.Open(filepath.Join(t.TempDir(), "frontier-v1.db"))
	if err != nil {
		t.Fatalf("open checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })

	return checkpoint
}

func TestConsumerRestoresHistoricalFullySeededRunWithoutRepeatingExpansion(t *testing.T) {
	checkpoint := openConsumerCheckpoint(t)
	order, identity := historicalCheckpointOrder(t, "active-recovery")
	observedAt := stageHistoricalSeededRun(t, checkpoint, order, identity)
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	tally := runtally.New()
	crawlFrontier := frontier.NewFrontier(
		2,
		nil,
		frontier.WithCheckpoint(checkpoint),
		frontier.WithRunTally(tally),
	)
	expanderCalled := make(chan struct{})
	reporter := &captureReporter{}
	consumer := crawlorder.NewCrawlOrderConsumer(
		queue,
		crawlFrontier,
		rejectingRecoveryExpander{called: expanderCalled},
	).WithProgressReporter(reporter).WithRunTally(tally)
	acked := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go consumer.Run(ctx)
	if err := queue.Publish(ctx, crawlorder.CrawlOrderDelivery{
		Order:         order,
		OrderIdentity: identity,
		Ack:           func(context.Context) error { close(acked); return nil },
		Nak: func(context.Context) error {
			t.Error("restored successful run was naked")

			return nil
		},
	}); err != nil {
		t.Fatalf("publish recovered order: %v", err)
	}
	job := receiveConsumerJob(t, crawlFrontier)
	deadline := time.Now().Add(time.Second)
	for len(reporter.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	reports := reporter.snapshot()
	if len(reports) == 0 || reports[0].State != yagocrawlcontract.CrawlRunRunning ||
		reports[0].Tally.Pending != 1 || reports[0].Tally.Duplicates != 3 {
		t.Fatalf("initial recovery report = %+v, want running pending 1 duplicates 3", reports)
	}
	if job.ObservationID != "restored-observation" || !job.ObservedAt.Equal(observedAt) {
		t.Fatalf("restored job = %#v", job)
	}
	select {
	case <-expanderCalled:
		t.Fatal("fully seeded recovery repeated request expansion")
	default:
	}
	crawlFrontier.Done(job, successfulPageOutcome())
	select {
	case <-acked:
	case <-time.After(time.Second):
		t.Fatal("restored run was not acknowledged")
	}
	waitCheckpointMissing(t, checkpoint, order.Provenance, identity)
}

func stageHistoricalSeededRun(
	t *testing.T,
	checkpoint *frontiercheckpoint.FrontierCheckpoint,
	order yagocrawlcontract.CrawlOrder,
	identity []byte,
) time.Time {
	t.Helper()
	if err := checkpoint.Begin(
		context.Background(),
		order.Provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("begin run: %v", err)
	}
	observedAt := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	if admitted, err := checkpoint.Admit(
		context.Background(),
		order.Provenance,
		[]frontiercheckpoint.Page{{
			URL:           order.Requests[0].URL,
			Host:          "example.com",
			Depth:         0,
			ProfileHandle: order.Profile.Handle,
			ObservationID: "restored-observation",
			ObservedAt:    observedAt,
			Index:         true,
		}},
	); err != nil ||
		admitted != 1 {
		t.Fatalf("admit restored page = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(
		context.Background(),
		order.Provenance,
		yagocrawlcontract.CrawlRunTally{Duplicates: 3},
	); err != nil {
		t.Fatalf("finish seeding: %v", err)
	}

	return observedAt
}

func TestConsumerSettlesCompletedCheckpointWithoutRefetch(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "ack", true: "ack-quarantined-failure"}[failed],
			func(t *testing.T) {
				runCompletedCheckpointRecovery(t, failed)
			},
		)
	}
}

func runCompletedCheckpointRecovery(t *testing.T, failed bool) {
	t.Helper()
	checkpoint := openConsumerCheckpoint(t)
	order, identity := checkpointOrder(t, "completed-recovery")
	stageCompletedCheckpoint(t, checkpoint, order, identity, failed)
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	crawlFrontier := frontier.NewFrontier(1, nil, frontier.WithCheckpoint(checkpoint))
	expanderCalled := make(chan struct{})
	consumer := crawlorder.NewCrawlOrderConsumer(
		queue,
		crawlFrontier,
		rejectingRecoveryExpander{called: expanderCalled},
	)
	settled := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go consumer.Run(ctx)
	if err := queue.Publish(ctx, crawlorder.CrawlOrderDelivery{
		Order: order,
		Ack:   func(context.Context) error { settled <- "ack"; return nil },
		Nak:   func(context.Context) error { settled <- "nak"; return nil },
	}); err != nil {
		t.Fatalf("publish completed order: %v", err)
	}
	select {
	case got := <-settled:
		if got != "ack" {
			t.Fatalf("settlement = %q, want ack", got)
		}
	case <-time.After(time.Second):
		t.Fatal("completed checkpoint was not settled")
	}
	select {
	case <-expanderCalled:
		t.Fatal("completed checkpoint repeated request expansion")
	default:
	}
	waitCheckpointMissing(t, checkpoint, order.Provenance, identity)
}

func stageCompletedCheckpoint(
	t *testing.T,
	checkpoint *frontiercheckpoint.FrontierCheckpoint,
	order yagocrawlcontract.CrawlOrder,
	identity []byte,
	failed bool,
) {
	t.Helper()
	if err := checkpoint.Begin(
		context.Background(),
		order.Provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("begin run: %v", err)
	}
	if !failed {
		if err := checkpoint.FinishSeeding(
			context.Background(),
			order.Provenance,
			yagocrawlcontract.CrawlRunTally{},
		); err != nil {
			t.Fatalf("complete empty run: %v", err)
		}

		return
	}
	page := frontiercheckpoint.Page{
		URL:           order.Requests[0].URL,
		Host:          "example.com",
		ProfileHandle: order.Profile.Handle,
		ObservationID: "failed-observation",
		ObservedAt:    time.Now().UTC(),
		Index:         true,
	}
	if _, err := checkpoint.Admit(
		context.Background(),
		order.Provenance,
		[]frontiercheckpoint.Page{page},
	); err != nil {
		t.Fatalf("admit failed page: %v", err)
	}
	if err := checkpoint.FinishSeeding(
		context.Background(),
		order.Provenance,
		yagocrawlcontract.CrawlRunTally{},
	); err != nil {
		t.Fatalf("finish failed seeding: %v", err)
	}
	if err := checkpoint.CompletePage(
		context.Background(),
		order.Provenance,
		page.URL,
		frontiercheckpoint.PageCompletion{
			Tally: yagocrawlcontract.CrawlRunTally{Failed: 1},
		},
	); err != nil {
		t.Fatalf("complete failed page: %v", err)
	}
}

func TestConsumerTerminatesPersistedCancellationWithoutRefetch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("open checkpoint: %v", err)
	}
	order, identity := checkpointOrder(t, "cancelled-recovery")
	if err := checkpoint.Begin(
		context.Background(),
		order.Provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("begin cancelled run: %v", err)
	}
	if err := checkpoint.UpdateControl(
		context.Background(),
		order.Provenance,
		frontiercheckpoint.ControlUpdate{Cancelled: true},
	); err != nil {
		t.Fatalf("cancel run: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint: %v", err)
	}
	checkpoint, err = frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("reopen checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	crawlFrontier := frontier.NewFrontier(1, nil, frontier.WithCheckpoint(checkpoint))
	expanderCalled := make(chan struct{})
	consumer := crawlorder.NewCrawlOrderConsumer(
		queue,
		crawlFrontier,
		rejectingRecoveryExpander{called: expanderCalled},
	)
	terminated := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go consumer.Run(ctx)
	if err := queue.Publish(ctx, crawlorder.CrawlOrderDelivery{
		Order: order,
		Ack:   func(context.Context) error { t.Error("cancelled order was acknowledged"); return nil },
		Nak:   func(context.Context) error { t.Error("cancelled order was requeued"); return nil },
		Term:  func(context.Context) error { close(terminated); return nil },
	}); err != nil {
		t.Fatalf("publish cancelled order: %v", err)
	}
	select {
	case <-terminated:
	case <-time.After(time.Second):
		t.Fatal("persisted cancellation was not terminated")
	}
	select {
	case <-expanderCalled:
		t.Fatal("persisted cancellation repeated request expansion")
	default:
	}
	waitCheckpointMissing(t, checkpoint, order.Provenance, identity)
}

func TestConsumerRetainsLegacyPartialCheckpointBeforeProfileCompilation(t *testing.T) {
	checkpoint := openConsumerCheckpoint(t)
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Scope:        yagocrawlcontract.ScopeDomain,
		URLMustMatch: "(",
	})
	order := yagocrawlcontract.CrawlOrder{
		Provenance: []byte("invalid-profile-recovery"),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:           "https://example.com/invalid-profile",
			ProfileHandle: profile.Handle,
		}},
	}
	encoded, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal invalid-profile order: %v", err)
	}
	identity := sha256.Sum256(encoded)
	if err := checkpoint.Begin(
		context.Background(),
		order.Provenance,
		identity[:],
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("begin invalid-profile run: %v", err)
	}
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	crawlFrontier := frontier.NewFrontier(1, nil, frontier.WithCheckpoint(checkpoint))
	consumer := crawlorder.NewCrawlOrderConsumer(queue, crawlFrontier)
	settled := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go consumer.Run(ctx)
	if err := queue.Publish(ctx, crawlorder.CrawlOrderDelivery{
		Order: order,
		Ack:   func(context.Context) error { settled <- "ack"; return nil },
		Nak:   func(context.Context) error { settled <- "nak"; return nil },
		Term:  func(context.Context) error { settled <- "term"; return nil },
	}); err != nil {
		t.Fatalf("publish invalid-profile order: %v", err)
	}
	queue.Close()
	select {
	case got := <-settled:
		t.Fatalf("legacy partial checkpoint sent %s", got)
	case <-time.After(20 * time.Millisecond):
	}
	state, err := checkpoint.Inspect(context.Background(), order.Provenance, identity[:])
	if err != nil || state.Status != frontiercheckpoint.RunActive || !state.Seeding ||
		state.SeedManifest {
		t.Fatalf("legacy partial checkpoint = %+v, %v", state, err)
	}
}

func waitCheckpointMissing(
	t *testing.T,
	checkpoint *frontiercheckpoint.FrontierCheckpoint,
	provenance []byte,
	identity []byte,
) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		status, err := checkpoint.Status(context.Background(), provenance, identity)
		if err == nil && status == frontiercheckpoint.RunMissing {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("checkpoint status = %v, %v, want missing", status, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func receiveConsumerJob(t *testing.T, crawlFrontier *frontier.Frontier) crawljob.CrawlJob {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	job, ok := crawlFrontier.Take(ctx)
	if !ok {
		t.Fatal("frontier did not return restored job")
	}

	return job
}
