package dhtexchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
)

type observedDistributions struct {
	receipts []DistributionReceipt
}

func (o *observedDistributions) Observe(receipt DistributionReceipt) {
	o.receipts = append(o.receipts, receipt)
}

type feedScript struct {
	receipt OutboundFeedReceipt
	err     error
	calls   int
}

func (s *feedScript) Feed(context.Context) (OutboundFeedReceipt, error) {
	s.calls++

	return s.receipt, s.err
}

type queueFeedScript struct {
	queue    *OutboundQueue
	peer     yagomodel.Seed
	postings []yagomodel.RWIPosting
	calls    int
}

func (s *queueFeedScript) Feed(context.Context) (OutboundFeedReceipt, error) {
	s.calls++
	if s.queue.Len() != 0 {
		return OutboundFeedReceipt{State: OutboundFeedSkipped}, nil
	}

	s.queue.add(s.peer, s.postings)

	return OutboundFeedReceipt{State: OutboundFeedEnqueued}, nil
}

type peerHandoffScript struct {
	receipts map[yagomodel.Hash]indextransfer.HandoffReceipt
	peers    []yagomodel.Hash
}

func (s *peerHandoffScript) Send(
	_ context.Context,
	peer yagomodel.Seed,
	_ []yagomodel.RWIPosting,
) (indextransfer.HandoffReceipt, error) {
	s.peers = append(s.peers, peer.Hash)

	return s.receipts[peer.Hash], nil
}

func TestOutboundSchedulerSendsReadyChunkAndObservesSuccess(t *testing.T) {
	t.Parallel()

	word := queueHash(t, "WWWWWWWWWWWW")
	peer := queueSeed(t, "AAAAAAAAAAAA")
	queue := NewOutboundQueue()
	queue.add(peer, []yagomodel.RWIPosting{
		queuePosting(word, yagomodel.WordHash("url-a")),
	})
	observer := &observedDistributions{}
	scheduler := NewOutboundScheduler(
		NewOutboundDistributor(
			queue,
			&handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)},
			&wordRestorerScript{},
		),
		NewOutboundRetryPolicy(OutboundRetryConfig{}),
		observer,
		func(context.Context) GateState { return openGateState() },
		OutboundSchedulerConfig{Gates: DefaultGateConfig()},
	)

	receipt, err := scheduler.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if receipt.Distribution.State != DistributionSent ||
		receipt.Distribution.Peer != peer.Hash ||
		receipt.Retry.Status != OutboundRetryCleared {
		t.Fatalf("receipt = %#v", receipt)
	}
	if len(observer.receipts) != 1 || observer.receipts[0].State != DistributionSent {
		t.Fatalf("observed = %#v", observer.receipts)
	}
}

func TestOutboundSchedulerDefersDelayedPeers(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	word := queueHash(t, "WWWWWWWWWWWW")
	peer := queueSeed(t, "BBBBBBBBBBBB")
	queue := NewOutboundQueue()
	queue.add(peer, []yagomodel.RWIPosting{
		queuePosting(word, yagomodel.WordHash("url-a")),
	})
	retry := NewOutboundRetryPolicy(OutboundRetryConfig{
		BaseDelay:          time.Minute,
		MaxDelay:           time.Hour,
		QuarantineFailures: 3,
		QuarantineDuration: time.Hour,
		DelayFraction:      func(yagomodel.Hash, int) float64 { return 0.5 },
	})
	retry.Observe(DistributionReceipt{State: DistributionHandoffFailed, Peer: peer.Hash}, at)
	handoff := &handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)}
	observer := &observedDistributions{}
	scheduler := NewOutboundScheduler(
		NewOutboundDistributor(queue, handoff, &wordRestorerScript{}),
		retry,
		observer,
		func(context.Context) GateState { return openGateState() },
		OutboundSchedulerConfig{
			Gates: DefaultGateConfig(),
			Now:   func() time.Time { return at },
		},
	)

	receipt, err := scheduler.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if receipt.Distribution.State != DistributionRetryDeferred ||
		receipt.Retry.Status != OutboundRetryIgnored {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.PostingCount() != 1 || handoff.calls != 0 {
		t.Fatalf("queue/handoff = %d/%d", queue.PostingCount(), handoff.calls)
	}
	if len(observer.receipts) != 1 || observer.receipts[0].State != DistributionRetryDeferred {
		t.Fatalf("observed = %#v", observer.receipts)
	}
}

func TestOutboundSchedulerRestoresAfterBoundedTransportFailures(t *testing.T) {
	t.Parallel()

	current := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	peer := queueSeed(t, "CCCCCCCCCCCC")
	queue := NewOutboundQueue()
	queue.add(peer, []yagomodel.RWIPosting{
		queuePosting(queueHash(t, "WWWWWWWWWWWW"), yagomodel.WordHash("url-a")),
	})
	restorer := &wordRestorerScript{restored: 1}
	observer := &observedDistributions{}
	scheduler := NewOutboundScheduler(
		NewOutboundDistributor(
			queue,
			&handoffScript{err: errors.New("transport failed")},
			restorer,
		),
		NewOutboundRetryPolicy(OutboundRetryConfig{
			BaseDelay:          time.Second,
			MaxDelay:           time.Minute,
			QuarantineFailures: 2,
			QuarantineDuration: time.Minute,
			DelayFraction:      func(yagomodel.Hash, int) float64 { return 1 },
		}),
		observer,
		func(context.Context) GateState { return openGateState() },
		OutboundSchedulerConfig{
			Gates: DefaultGateConfig(),
			Now:   func() time.Time { return current },
		},
	)

	first, err := scheduler.RunOnce(context.Background())
	if err == nil || first.Retry.Status != OutboundRetryDelayed || queue.PostingCount() != 1 {
		t.Fatalf("first = %#v error=%v queue=%d", first, err, queue.PostingCount())
	}
	current = first.Retry.RetryAfter
	second, err := scheduler.RunOnce(context.Background())
	if err == nil ||
		second.Retry.Status != OutboundRetryQuarantined ||
		second.Distribution.RequeuedPostings != 0 ||
		second.Distribution.RestoredPostings != 1 ||
		queue.PostingCount() != 0 ||
		restorer.calls != 1 {
		t.Fatalf(
			"second = %#v error=%v queue=%d restore=%#v",
			second,
			err,
			queue.PostingCount(),
			restorer,
		)
	}
	if len(observer.receipts) != 2 || observer.receipts[1].RestoredPostings != 1 {
		t.Fatalf("observed = %#v", observer.receipts)
	}
}

func TestOutboundSchedulerRetainsChunkWhenBoundedRestoreFails(t *testing.T) {
	t.Parallel()

	peer := queueSeed(t, "DDDDDDDDDDDD")
	queue := NewOutboundQueue()
	queue.add(peer, []yagomodel.RWIPosting{
		queuePosting(queueHash(t, "WWWWWWWWWWWW"), yagomodel.WordHash("url-a")),
	})
	transportErr := errors.New("transport failed")
	restoreErr := errors.New("restore failed")
	scheduler := NewOutboundScheduler(
		NewOutboundDistributor(
			queue,
			&handoffScript{err: transportErr},
			&wordRestorerScript{err: restoreErr},
		),
		NewOutboundRetryPolicy(OutboundRetryConfig{
			BaseDelay:          time.Second,
			MaxDelay:           time.Minute,
			QuarantineFailures: 1,
			QuarantineDuration: time.Minute,
		}),
		&observedDistributions{},
		func(context.Context) GateState { return openGateState() },
		OutboundSchedulerConfig{Gates: DefaultGateConfig()},
	)

	receipt, err := scheduler.RunOnce(context.Background())
	if !errors.Is(err, transportErr) ||
		!errors.Is(err, restoreErr) ||
		receipt.Distribution.RequeuedPostings != 1 ||
		receipt.Distribution.RestoredPostings != 0 ||
		queue.PostingCount() != 1 {
		t.Fatalf("receipt/error/queue = %#v/%v/%d", receipt, err, queue.PostingCount())
	}
}

func TestOutboundSchedulerRejectedPeerDoesNotPinFreshFeed(t *testing.T) {
	t.Parallel()

	bad := queueSeed(t, "AAAAAAAAAAAA")
	good := queueSeed(t, "BBBBBBBBBBBB")
	postings := []yagomodel.RWIPosting{
		queuePosting(queueHash(t, "WWWWWWWWWWWW"), yagomodel.WordHash("url-a")),
	}
	queue := NewOutboundQueue()
	queue.add(bad, postings)
	restorer := &wordRestorerScript{restored: 1}
	handoff := &peerHandoffScript{receipts: map[yagomodel.Hash]indextransfer.HandoffReceipt{
		bad.Hash:  {State: indextransfer.HandoffRWIRejected},
		good.Hash: acceptedHandoff(indextransfer.HandoffRWIOnly),
	}}
	feed := &queueFeedScript{queue: queue, peer: good, postings: postings}
	scheduler := NewOutboundScheduler(
		NewOutboundDistributor(queue, handoff, restorer),
		NewOutboundRetryPolicy(OutboundRetryConfig{}),
		&observedDistributions{},
		func(context.Context) GateState { return openGateState() },
		OutboundSchedulerConfig{Gates: DefaultGateConfig(), Feed: feed},
	)

	first, err := scheduler.RunOnce(context.Background())
	if err != nil ||
		first.Feed.State != OutboundFeedSkipped ||
		first.Distribution.State != DistributionHandoffRejected ||
		first.Distribution.RestoredPostings != 1 ||
		queue.PostingCount() != 0 {
		t.Fatalf("first = %#v error=%v queue=%d", first, err, queue.PostingCount())
	}
	second, err := scheduler.RunOnce(context.Background())
	if err != nil ||
		second.Feed.State != OutboundFeedEnqueued ||
		second.Distribution.State != DistributionSent ||
		second.Distribution.Peer != good.Hash ||
		queue.PostingCount() != 0 ||
		len(handoff.peers) != 2 ||
		handoff.peers[0] != bad.Hash ||
		handoff.peers[1] != good.Hash {
		t.Fatalf(
			"second = %#v error=%v queue=%d handoff=%#v",
			second,
			err,
			queue.PostingCount(),
			handoff,
		)
	}
}

func TestOutboundSchedulerRunsFeederBeforeDistribution(t *testing.T) {
	t.Parallel()

	feed := &feedScript{receipt: OutboundFeedReceipt{State: OutboundFeedEmpty}}
	observer := &observedDistributions{}
	scheduler := NewOutboundScheduler(
		NewOutboundDistributor(
			NewOutboundQueue(),
			&handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)},
			&wordRestorerScript{},
		),
		NewOutboundRetryPolicy(OutboundRetryConfig{}),
		observer,
		func(context.Context) GateState { return openGateState() },
		OutboundSchedulerConfig{Gates: DefaultGateConfig(), Feed: feed},
	)

	receipt, err := scheduler.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if feed.calls != 1 ||
		receipt.Feed.State != OutboundFeedEmpty ||
		receipt.Distribution.State != DistributionQueueEmpty {
		t.Fatalf("receipt/feed = %#v/%d", receipt, feed.calls)
	}
}

func TestOutboundSchedulerDoesNotFeedWhenGatesAreClosed(t *testing.T) {
	t.Parallel()

	feed := &feedScript{receipt: OutboundFeedReceipt{State: OutboundFeedEnqueued}}
	closed := openGateState()
	closed.OnlineCaution = "proxy"
	scheduler := NewOutboundScheduler(
		NewOutboundDistributor(
			NewOutboundQueue(),
			&handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)},
			&wordRestorerScript{},
		),
		NewOutboundRetryPolicy(OutboundRetryConfig{}),
		&observedDistributions{},
		func(context.Context) GateState { return closed },
		OutboundSchedulerConfig{Gates: DefaultGateConfig(), Feed: feed},
	)

	receipt, err := scheduler.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if feed.calls != 0 ||
		receipt.Feed.State != "" ||
		receipt.Distribution.State != DistributionGateClosed {
		t.Fatalf("receipt/feed = %#v/%d", receipt, feed.calls)
	}
}

func TestOutboundSchedulerReturnsFeederErrorBeforeDistribution(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("feed failed")
	feed := &feedScript{
		receipt: OutboundFeedReceipt{State: OutboundFeedRestored, RestoredPostings: 2},
		err:     sentinel,
	}
	observer := &observedDistributions{}
	scheduler := NewOutboundScheduler(
		NewOutboundDistributor(
			NewOutboundQueue(),
			&handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)},
			&wordRestorerScript{},
		),
		NewOutboundRetryPolicy(OutboundRetryConfig{}),
		observer,
		func(context.Context) GateState { return openGateState() },
		OutboundSchedulerConfig{Gates: DefaultGateConfig(), Feed: feed},
	)

	receipt, err := scheduler.RunOnce(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunOnce error = %v, want %v", err, sentinel)
	}
	if receipt.Feed.State != OutboundFeedRestored ||
		receipt.Feed.RestoredPostings != 2 ||
		len(observer.receipts) != 0 {
		t.Fatalf("receipt/observer = %#v/%#v", receipt, observer)
	}
}
