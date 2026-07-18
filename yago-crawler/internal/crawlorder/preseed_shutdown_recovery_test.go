package crawlorder_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlorder"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yago-crawler/internal/runtally"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type inspectionContextObservation struct {
	bounded bool
	live    bool
}

type delayedInspectionCheckpoint struct {
	frontier.Checkpoint
	state       frontiercheckpoint.RunState
	started     chan struct{}
	release     chan struct{}
	observation chan inspectionContextObservation
}

func (checkpoint delayedInspectionCheckpoint) Inspect(
	ctx context.Context,
	_ []byte,
	_ []byte,
) (frontiercheckpoint.RunState, error) {
	close(checkpoint.started)
	<-checkpoint.release
	_, bounded := ctx.Deadline()
	checkpoint.observation <- inspectionContextObservation{
		bounded: bounded,
		live:    ctx.Err() == nil,
	}

	return checkpoint.state, nil
}

func TestShutdownDuringCheckpointInspectionRetainsDelivery(t *testing.T) {
	checkpoint := delayedInspectionCheckpoint{
		state: frontiercheckpoint.RunState{
			Status:  frontiercheckpoint.RunActive,
			Seeding: true,
			Pending: 1,
		},
		started:     make(chan struct{}),
		release:     make(chan struct{}),
		observation: make(chan inspectionContextObservation, 1),
	}
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	crawlFrontier := frontier.NewFrontier(1, nil, frontier.WithCheckpoint(checkpoint))
	expanderCalled := make(chan struct{})
	consumer := crawlorder.NewCrawlOrderConsumer(
		queue,
		crawlFrontier,
		rejectingRecoveryExpander{called: expanderCalled},
	)
	order, identity := checkpointOrder(t, "inspection-shutdown")
	settlement := make(chan string, 3)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		consumer.Run(ctx)
		close(done)
	}()
	if err := queue.Publish(ctx, crawlorder.CrawlOrderDelivery{
		LeaseID:       "inspection-lease",
		Order:         order,
		OrderIdentity: identity,
		Ack:           func(context.Context) error { settlement <- "ack"; return nil },
		Nak:           func(context.Context) error { settlement <- "nak"; return nil },
		Term:          func(context.Context) error { settlement <- "term"; return nil },
	}); err != nil {
		t.Fatalf("publish inspected delivery: %v", err)
	}
	select {
	case <-checkpoint.started:
	case <-time.After(time.Second):
		t.Fatal("checkpoint inspection did not start")
	}
	cancel()
	close(checkpoint.release)
	select {
	case observation := <-checkpoint.observation:
		if !observation.bounded || !observation.live {
			t.Fatalf(
				"inspection context = %+v, want bounded and live after parent cancellation",
				observation,
			)
		}
	case <-time.After(time.Second):
		t.Fatal("checkpoint inspection did not complete")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop after retained inspection")
	}
	select {
	case got := <-settlement:
		t.Fatalf("shutdown inspection sent %s", got)
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case <-expanderCalled:
		t.Fatal("shutdown inspection reached expansion")
	default:
	}
	if err := crawlFrontier.CheckpointFailure(); err != nil {
		t.Fatalf("shutdown inspection recorded storage failure: %v", err)
	}
}

type unavailableRecoveryExpander struct {
	calls atomic.Int32
}

func (expander *unavailableRecoveryExpander) Expand(
	context.Context,
	[]yagocrawlcontract.CrawlRequest,
) ([]yagocrawlcontract.CrawlRequest, error) {
	expander.calls.Add(1)
	return nil, errors.New("seed source is unavailable")
}

func TestLegacyPartialSeedingCheckpointRetainsWithoutExpansion(t *testing.T) {
	checkpoint := openConsumerCheckpoint(t)
	order, identity := checkpointOrder(t, "legacy-partial-seeding")
	if err := checkpoint.Begin(
		context.Background(),
		order.Provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("begin legacy partial checkpoint: %v", err)
	}
	page := frontiercheckpoint.Page{
		URL:           order.Requests[0].URL,
		Host:          "example.com",
		ProfileHandle: order.Profile.Handle,
		ObservationID: "legacy-observation",
		ObservedAt:    time.Date(2026, 7, 17, 1, 0, 0, 0, time.UTC),
		Index:         true,
	}
	if admitted, err := checkpoint.Admit(
		context.Background(),
		order.Provenance,
		[]frontiercheckpoint.Page{page},
	); err != nil || admitted != 1 {
		t.Fatalf("admit legacy partial page = %d, %v", admitted, err)
	}
	expander := &unavailableRecoveryExpander{}
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	crawlFrontier := frontier.NewFrontier(1, nil, frontier.WithCheckpoint(checkpoint))
	consumer := crawlorder.NewCrawlOrderConsumer(queue, crawlFrontier, expander)
	settlement := make(chan string, 3)
	done := make(chan struct{})
	go func() {
		consumer.Run(context.Background())
		close(done)
	}()
	if err := queue.Publish(context.Background(), crawlorder.CrawlOrderDelivery{
		LeaseID:       "legacy-partial-lease",
		Order:         order,
		OrderIdentity: identity,
		Ack:           func(context.Context) error { settlement <- "ack"; return nil },
		Nak:           func(context.Context) error { settlement <- "nak"; return nil },
		Term:          func(context.Context) error { settlement <- "term"; return nil },
	}); err != nil {
		t.Fatalf("publish legacy partial checkpoint: %v", err)
	}
	queue.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("legacy partial consumer did not stop")
	}
	if expander.calls.Load() != 0 {
		t.Fatalf("legacy partial expansion calls = %d, want 0", expander.calls.Load())
	}
	select {
	case got := <-settlement:
		t.Fatalf("legacy partial checkpoint sent %s", got)
	case <-time.After(20 * time.Millisecond):
	}
	state, err := checkpoint.Inspect(context.Background(), order.Provenance, identity)
	if err != nil || state.Status != frontiercheckpoint.RunActive || !state.Seeding ||
		state.SeedManifest || state.Pending != 1 {
		t.Fatalf("legacy partial checkpoint = %+v, %v", state, err)
	}
}

func durableManifestPages(
	profileHandle string,
	observedAt time.Time,
) []frontiercheckpoint.Page {
	pages := make([]frontiercheckpoint.Page, 512)
	for index := range pages {
		pages[index] = frontiercheckpoint.Page{
			URL:           fmt.Sprintf("https://example.com/manifest/%03d", index),
			Host:          "example.com",
			ProfileHandle: profileHandle,
			ObservationID: fmt.Sprintf("manifest-observation-%03d", index),
			ObservedAt:    observedAt.Add(time.Duration(index) * time.Second),
			Index:         true,
		}
	}
	return pages
}

func TestManifestedRecoveryResumesExactSuffixWithoutExpansion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	order, identity, pages := persistInterruptedSeedManifest(t, path)
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("reopen crash-cut checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	expander := &unavailableRecoveryExpander{}
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	tally := runtally.New()
	crawlFrontier := frontier.NewFrontier(
		512,
		nil,
		frontier.WithCheckpoint(checkpoint),
		frontier.WithRunTally(tally),
	)
	reporter := &captureReporter{}
	consumer := crawlorder.NewCrawlOrderConsumer(
		queue,
		crawlFrontier,
		expander,
	).WithProgressReporter(reporter).WithRunTally(tally)
	settlement := make(chan string, 3)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go consumer.Run(ctx)
	if err := queue.Publish(ctx, crawlorder.CrawlOrderDelivery{
		LeaseID:       "manifested-suffix-lease",
		Order:         order,
		OrderIdentity: identity,
		Ack:           func(context.Context) error { settlement <- "ack"; return nil },
		Nak:           func(context.Context) error { settlement <- "nak"; return nil },
		Term:          func(context.Context) error { settlement <- "term"; return nil },
	}); err != nil {
		t.Fatalf("publish manifested suffix: %v", err)
	}
	finishRestoredManifestSuffix(t, crawlFrontier, pages)
	expectManifestSettlement(t, settlement)
	if expander.calls.Load() != 0 {
		t.Fatalf("manifested recovery expansion calls = %d, want 0", expander.calls.Load())
	}
	if got := tally.Snapshot(order.Provenance); got != (yagocrawlcontract.CrawlRunTally{}) {
		t.Fatalf("forgotten manifested tally = %+v", got)
	}
	waitCheckpointMissing(t, checkpoint, order.Provenance, identity)
}

func persistInterruptedSeedManifest(
	t *testing.T,
	path string,
) (yagocrawlcontract.CrawlOrder, []byte, []frontiercheckpoint.Page) {
	t.Helper()
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("open manifest checkpoint: %v", err)
	}
	order, identity := checkpointOrder(t, "manifested-suffix")
	pages := durableManifestPages(
		order.Profile.Handle,
		time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC),
	)
	if err := checkpoint.BeginSeedManifest(
		context.Background(),
		order.Provenance,
		identity,
		yagocrawlcontract.CrawlOrderPriorityNormal,
		pages,
	); err != nil {
		t.Fatalf("persist complete seed manifest: %v", err)
	}
	persistSeedManifestPrefix(t, checkpoint, order, pages)
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close crash-cut checkpoint: %v", err)
	}

	return order, identity, pages
}

func persistSeedManifestPrefix(
	t *testing.T,
	checkpoint *frontiercheckpoint.FrontierCheckpoint,
	order yagocrawlcontract.CrawlOrder,
	pages []frontiercheckpoint.Page,
) {
	t.Helper()
	decisions := make([]frontiercheckpoint.SeedDecision, 256)
	for index := range decisions {
		decisions[index] = frontiercheckpoint.SeedDecision{Page: pages[index], Admit: true}
	}
	result, err := checkpoint.AdmitSeedBatch(
		context.Background(),
		order.Provenance,
		frontiercheckpoint.SeedBatch{Decisions: decisions},
	)
	if err != nil || result.Admitted != 256 || result.Duplicates != 0 {
		t.Fatalf("first durable seed batch = %+v, %v", result, err)
	}
	for _, page := range pages[:256] {
		if err := checkpoint.CompletePage(
			context.Background(),
			order.Provenance,
			page.URL,
			frontiercheckpoint.PageCompletion{
				Tally: yagocrawlcontract.CrawlRunTally{Fetched: 1},
			},
		); err != nil {
			t.Fatalf("complete first batch page %q: %v", page.URL, err)
		}
	}
	snapshot, err := checkpoint.Load(context.Background(), order.Provenance)
	if err != nil || !snapshot.Seeding || !snapshot.SeedManifest ||
		snapshot.SeedCursor != 256 || snapshot.Counters.Pending != 0 ||
		snapshot.Tally.Fetched != 256 || len(snapshot.SeedPages) != 512 {
		t.Fatalf("crash-cut manifest snapshot = %+v, %v", snapshot, err)
	}
}

func finishRestoredManifestSuffix(
	t *testing.T,
	crawlFrontier *frontier.Frontier,
	pages []frontiercheckpoint.Page,
) {
	t.Helper()
	wantObservations := make(map[string]string, 256)
	for _, page := range pages[256:] {
		wantObservations[page.URL] = page.ObservationID
	}
	for range 256 {
		job := receiveConsumerJob(t, crawlFrontier)
		if wantObservations[job.URL] != job.ObservationID {
			t.Fatalf("restored suffix job = %#v", job)
		}
		delete(wantObservations, job.URL)
		crawlFrontier.Done(job, successfulPageOutcome())
	}
	if len(wantObservations) != 0 {
		t.Fatalf("restored suffix missing %d pages", len(wantObservations))
	}
}

func expectManifestSettlement(t *testing.T, settlement <-chan string) {
	t.Helper()
	select {
	case got := <-settlement:
		if got != "ack" {
			t.Fatalf("manifested suffix settlement = %q, want ack", got)
		}
	case <-time.After(time.Second):
		t.Fatal("manifested suffix was not acknowledged")
	}
}
