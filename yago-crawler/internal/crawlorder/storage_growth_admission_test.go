package crawlorder_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlorder"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type rejectingGrowthAdmission struct {
	called chan struct{}
}

func (admission rejectingGrowthAdmission) WaitForGrowth(context.Context) (bool, error) {
	close(admission.called)

	return false, nil
}

type unexpectedGrowthAdmission struct{}

func (unexpectedGrowthAdmission) WaitForGrowth(context.Context) (bool, error) {
	panic("recovered crawl order reached new-growth admission")
}

type failedGrowthAdmission struct {
	err    error
	called chan struct{}
}

type checkpointGrowthAdmission struct {
	checkpoint *frontiercheckpoint.FrontierCheckpoint
	called     chan struct{}
}

func (admission checkpointGrowthAdmission) WaitForGrowth(
	ctx context.Context,
) (bool, error) {
	admission.called <- struct{}{}

	allowed, err := admission.checkpoint.WaitForGrowth(ctx)
	if err != nil {
		return allowed, fmt.Errorf("wait for checkpoint growth: %w", err)
	}

	return allowed, nil
}

type observedPassThroughExpander struct {
	called chan struct{}
}

func (expander observedPassThroughExpander) Expand(
	_ context.Context,
	requests []yagocrawlcontract.CrawlRequest,
) ([]yagocrawlcontract.CrawlRequest, error) {
	expander.called <- struct{}{}

	return requests, nil
}

func (admission failedGrowthAdmission) WaitForGrowth(context.Context) (bool, error) {
	close(admission.called)

	return false, admission.err
}

type observedRequestExpander struct {
	called chan struct{}
}

func (expander observedRequestExpander) Expand(
	context.Context,
	[]yagocrawlcontract.CrawlRequest,
) ([]yagocrawlcontract.CrawlRequest, error) {
	close(expander.called)

	return nil, nil
}

func TestNewOrderWaitsForStorageBeforeExpansion(t *testing.T) {
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	crawlFrontier := frontier.NewFrontier(1, nil)
	admissionCalled := make(chan struct{})
	expanderCalled := make(chan struct{})
	consumer := crawlorder.NewCrawlOrderConsumer(
		queue,
		crawlFrontier,
		observedRequestExpander{called: expanderCalled},
	).WithGrowthAdmission(rejectingGrowthAdmission{called: admissionCalled})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go consumer.Run(ctx)
	order, _ := checkpointOrder(t, "storage-pressure-new")
	if err := queue.Publish(ctx, crawlorder.CrawlOrderDelivery{
		Order: order,
	}); err != nil {
		t.Fatalf("publish crawl order: %v", err)
	}
	select {
	case <-admissionCalled:
	case <-time.After(time.Second):
		t.Fatal("new crawl order did not reach storage admission")
	}
	select {
	case <-expanderCalled:
		t.Fatal("storage-blocked crawl order reached expansion")
	default:
	}
	queue.Close()
}

func TestCompletedCheckpointBypassesNewGrowthAdmission(t *testing.T) {
	checkpoint := openConsumerCheckpoint(t)
	order, identity := checkpointOrder(t, "storage-pressure-recovery")
	stageCompletedCheckpoint(t, checkpoint, order, identity, false)
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	consumer := crawlorder.NewCrawlOrderConsumer(
		queue,
		frontier.NewFrontier(1, nil, frontier.WithCheckpoint(checkpoint)),
	).WithGrowthAdmission(unexpectedGrowthAdmission{})
	settled := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go consumer.Run(ctx)
	if err := queue.Publish(ctx, crawlorder.CrawlOrderDelivery{
		Order: order,
		Ack: func(context.Context) error {
			close(settled)

			return nil
		},
	}); err != nil {
		t.Fatalf("publish recovered order: %v", err)
	}
	select {
	case <-settled:
	case <-time.After(time.Second):
		t.Fatal("completed checkpoint did not settle")
	}
	queue.Close()
}

func TestNewOrderGrowthFailureRetainsOrderAndStopsCrawler(t *testing.T) {
	want := errors.New("inspect crawler state")
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	shutdown := make(chan struct{})
	crawlFrontier := frontier.NewFrontier(
		1,
		nil,
		frontier.WithCheckpointFailureShutdown(func() { close(shutdown) }),
	)
	admissionCalled := make(chan struct{})
	expanderCalled := make(chan struct{})
	consumer := crawlorder.NewCrawlOrderConsumer(
		queue,
		crawlFrontier,
		observedRequestExpander{called: expanderCalled},
	).WithGrowthAdmission(failedGrowthAdmission{err: want, called: admissionCalled})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go consumer.Run(ctx)
	order, _ := checkpointOrder(t, "state-inspection-failure")
	if err := queue.Publish(ctx, crawlorder.CrawlOrderDelivery{Order: order}); err != nil {
		t.Fatalf("publish crawl order: %v", err)
	}
	select {
	case <-admissionCalled:
	case <-time.After(time.Second):
		t.Fatal("new crawl order did not reach growth admission")
	}
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Fatal("growth inspection failure did not stop the crawler")
	}
	if !errors.Is(crawlFrontier.CheckpointFailure(), want) {
		t.Fatalf("crawler checkpoint failure = %v", crawlFrontier.CheckpointFailure())
	}
	select {
	case <-expanderCalled:
		t.Fatal("growth inspection failure reached expansion")
	default:
	}
	queue.Close()
}

type stateMaximumCrossingScenario struct {
	checkpoint      *frontiercheckpoint.FrontierCheckpoint
	queue           *boundedqueue.BoundedQueue[crawlorder.CrawlOrderDelivery]
	crawlFrontier   *frontier.Frontier
	ctx             context.Context
	order           yagocrawlcontract.CrawlOrder
	orderIdentity   []byte
	admissionCalled chan struct{}
	expanderCalled  chan struct{}
}

func newStateMaximumCrossingScenario(t *testing.T) *stateMaximumCrossingScenario {
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("open frontier checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inspect frontier checkpoint: %v", err)
	}
	stateBytes, err := strconv.ParseUint(strconv.FormatInt(info.Size(), 10), 10, 64)
	if err != nil {
		t.Fatalf("parse frontier checkpoint size: %v", err)
	}
	checkpoint.SetStateMaximumBytes(stateBytes + 1)
	if err := checkpoint.CheckGrowth(); err != nil {
		t.Fatalf("growth before order = %v", err)
	}
	order, identity := stateMaximumCrossingOrder(t)
	queue := boundedqueue.NewBoundedQueue[crawlorder.CrawlOrderDelivery](1)
	crawlFrontier := frontier.NewFrontier(
		64,
		nil,
		frontier.WithCheckpoint(checkpoint),
		frontier.WithStateGrowthAdmission(checkpoint),
	)
	admissionCalled := make(chan struct{}, 2)
	expanderCalled := make(chan struct{}, 2)
	consumer := crawlorder.NewCrawlOrderConsumer(
		queue,
		crawlFrontier,
		observedPassThroughExpander{called: expanderCalled},
	).
		WithGrowthAdmission(checkpointGrowthAdmission{
			checkpoint: checkpoint,
			called:     admissionCalled,
		})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go consumer.Run(ctx)

	return &stateMaximumCrossingScenario{
		checkpoint:      checkpoint,
		queue:           queue,
		crawlFrontier:   crawlFrontier,
		ctx:             ctx,
		order:           order,
		orderIdentity:   identity,
		admissionCalled: admissionCalled,
		expanderCalled:  expanderCalled,
	}
}

func stateMaximumCrossingOrder(
	t *testing.T,
) (yagocrawlcontract.CrawlOrder, []byte) {
	order, _ := checkpointOrder(t, "manifest-crosses-maximum")
	order.Requests = make([]yagocrawlcontract.CrawlRequest, 0, 512)
	for index := range 512 {
		order.Requests = append(order.Requests, yagocrawlcontract.CrawlRequest{
			URL: fmt.Sprintf(
				"https://example.com/%03d-%s",
				index,
				strings.Repeat("x", 192),
			),
			ProfileHandle: order.Profile.Handle,
		})
	}
	encoded, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal crawl order: %v", err)
	}
	identity := sha256.Sum256(encoded)

	return order, identity[:]
}

func (scenario *stateMaximumCrossingScenario) requireCrossingOrderCompletion(
	t *testing.T,
) {
	acknowledged := make(chan struct{}, 1)
	requeued := make(chan struct{}, 1)
	if err := scenario.queue.Publish(scenario.ctx, crawlorder.CrawlOrderDelivery{
		LeaseID:       "manifest-crosses-maximum",
		Order:         scenario.order,
		OrderIdentity: scenario.orderIdentity,
		Ack: func(context.Context) error {
			acknowledged <- struct{}{}

			return nil
		},
		Nak: func(context.Context) error {
			requeued <- struct{}{}

			return nil
		},
	}); err != nil {
		t.Fatalf("publish crawl order: %v", err)
	}
	select {
	case <-scenario.admissionCalled:
	case <-time.After(time.Second):
		t.Fatal("crawl order did not reach state admission")
	}
	select {
	case <-scenario.expanderCalled:
	case <-time.After(time.Second):
		t.Fatal("admitted crawl order did not reach expansion")
	}
	for range scenario.order.Requests {
		jobCtx, cancelJob := context.WithTimeout(t.Context(), 2*time.Second)
		job, ok := scenario.crawlFrontier.Take(jobCtx)
		cancelJob()
		if !ok {
			t.Fatal("seed manifest did not remain dispatchable after crossing the maximum")
		}
		scenario.crawlFrontier.Done(job, yagocrawlcontract.CrawlRunTally{})
	}
	select {
	case <-acknowledged:
	case <-time.After(2 * time.Second):
		t.Fatal("crossing seed manifest did not complete")
	}
	select {
	case <-requeued:
		t.Fatal("crossing seed manifest was requeued")
	default:
	}
	if err := scenario.checkpoint.CheckGrowth(); !errors.Is(
		err,
		frontiercheckpoint.ErrStateMaximum,
	) {
		t.Fatalf("post-manifest growth error = %v", err)
	}
}

func (scenario *stateMaximumCrossingScenario) requireFreshOrderResume(
	t *testing.T,
) {
	secondOrder, secondIdentity := checkpointOrder(t, "fresh-order-after-maximum")
	secondAcknowledged := make(chan struct{}, 1)
	if err := scenario.queue.Publish(scenario.ctx, crawlorder.CrawlOrderDelivery{
		LeaseID:       "fresh-order-after-maximum",
		Order:         secondOrder,
		OrderIdentity: secondIdentity,
		Ack: func(context.Context) error {
			secondAcknowledged <- struct{}{}

			return nil
		},
	}); err != nil {
		t.Fatalf("publish second crawl order: %v", err)
	}
	select {
	case <-scenario.admissionCalled:
	case <-time.After(time.Second):
		t.Fatal("second crawl order did not reach state admission")
	}
	select {
	case <-scenario.expanderCalled:
		t.Fatal("fresh order crossed the state maximum")
	case <-time.After(50 * time.Millisecond):
	}
	scenario.checkpoint.SetStateMaximumBytes(1 << 30)
	select {
	case <-scenario.expanderCalled:
	case <-time.After(time.Second):
		t.Fatal("fresh order did not resume after the live maximum raise")
	}
	jobCtx, cancelJob := context.WithTimeout(t.Context(), 2*time.Second)
	job, ok := scenario.crawlFrontier.Take(jobCtx)
	cancelJob()
	if !ok {
		t.Fatal("resumed crawl order did not become dispatchable")
	}
	scenario.crawlFrontier.Done(job, yagocrawlcontract.CrawlRunTally{})
	select {
	case <-secondAcknowledged:
	case <-time.After(2 * time.Second):
		t.Fatal("resumed crawl order did not complete")
	}
}

func TestOrderWhoseSeedManifestCrossesStateMaximumCompletes(t *testing.T) {
	scenario := newStateMaximumCrossingScenario(t)
	scenario.requireCrossingOrderCompletion(t)
	scenario.requireFreshOrderResume(t)
	scenario.queue.Close()
}
