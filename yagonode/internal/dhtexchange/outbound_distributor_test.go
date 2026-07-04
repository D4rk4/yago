package dhtexchange

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
)

type capacityScript struct {
	count int
	err   error
	calls int
	peer  yagomodel.Hash
}

func (s *capacityScript) RWICount(
	_ context.Context,
	peer yagomodel.Seed,
) (int, error) {
	s.calls++
	s.peer = peer.Hash
	if s.err != nil {
		return 0, s.err
	}

	return s.count, nil
}

type handoffScript struct {
	receipt  indextransfer.HandoffReceipt
	err      error
	calls    int
	peer     yagomodel.Hash
	postings []yagomodel.RWIPosting
}

func (s *handoffScript) Send(
	_ context.Context,
	peer yagomodel.Seed,
	postings []yagomodel.RWIPosting,
) (indextransfer.HandoffReceipt, error) {
	s.calls++
	s.peer = peer.Hash
	s.postings = append([]yagomodel.RWIPosting(nil), postings...)
	if s.err != nil {
		return indextransfer.HandoffReceipt{}, s.err
	}

	return s.receipt, nil
}

type sentPostingConfirmerScript struct {
	confirmed int
	err       error
	calls     int
	postings  []yagomodel.RWIPosting
}

func (s *sentPostingConfirmerScript) ConfirmTransferred(
	_ context.Context,
	postings []yagomodel.RWIPosting,
) (int, error) {
	s.calls++
	s.postings = append([]yagomodel.RWIPosting(nil), postings...)
	if s.err != nil {
		return 0, s.err
	}

	return s.confirmed, nil
}

func TestOutboundDistributorStopsWhenGatesAreClosed(t *testing.T) {
	t.Parallel()

	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{
		queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url")),
	})
	probe := &capacityScript{count: 12}
	handoff := &handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)}
	state := openGateState()
	state.PublicReachable = false

	receipt, err := NewOutboundDistributor(queue, probe, handoff).
		Distribute(context.Background(), state, DefaultGateConfig())
	if err != nil {
		t.Fatalf("Distribute: %v", err)
	}

	if receipt.State != DistributionGateClosed ||
		receipt.Gates.BlockingReason != GatePublicReachabilityReason {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.PostingCount() != 1 || probe.calls != 0 || handoff.calls != 0 {
		t.Fatalf(
			"queue/probe/handoff = %d/%d/%d, want untouched",
			queue.PostingCount(),
			probe.calls,
			handoff.calls,
		)
	}
}

func TestOutboundDistributorReportsEmptyQueue(t *testing.T) {
	t.Parallel()

	probe := &capacityScript{count: 12}
	handoff := &handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)}
	receipt, err := NewOutboundDistributor(NewOutboundQueue(), probe, handoff).
		Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("Distribute: %v", err)
	}

	if receipt.State != DistributionQueueEmpty || probe.calls != 0 || handoff.calls != 0 {
		t.Fatalf("receipt/probe/handoff = %#v/%d/%d", receipt, probe.calls, handoff.calls)
	}
}

func TestOutboundDistributorProbesAndSendsLargestChunk(t *testing.T) {
	t.Parallel()

	word := yagomodel.WordHash("word")
	largest := []yagomodel.RWIPosting{
		queuePosting(word, yagomodel.WordHash("url-a")),
		queuePosting(word, yagomodel.WordHash("url-b")),
	}
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "BBBBBBBBBBBB"), []yagomodel.RWIPosting{
		queuePosting(word, yagomodel.WordHash("url-c")),
	})
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), largest)
	probe := &capacityScript{count: 321}
	handoff := &handoffScript{receipt: acceptedHandoff(indextransfer.HandoffURLSent)}

	receipt, err := NewOutboundDistributor(queue, probe, handoff).
		Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("Distribute: %v", err)
	}

	if receipt.State != DistributionSent ||
		receipt.Peer != queueHash(t, "AAAAAAAAAAAA") ||
		receipt.Target.Hash != queueHash(t, "AAAAAAAAAAAA") ||
		receipt.PostingCount != 2 ||
		receipt.RemoteRWIWords != 321 ||
		receipt.Handoff.State != indextransfer.HandoffURLSent {
		t.Fatalf("receipt = %#v", receipt)
	}
	if probe.calls != 1 || probe.peer != queueHash(t, "AAAAAAAAAAAA") {
		t.Fatalf("probe = calls %d peer %q", probe.calls, probe.peer)
	}
	if handoff.calls != 1 ||
		handoff.peer != queueHash(t, "AAAAAAAAAAAA") ||
		!reflect.DeepEqual(handoff.postings, largest) {
		t.Fatalf(
			"handoff = calls %d peer %q postings %#v",
			handoff.calls,
			handoff.peer,
			handoff.postings,
		)
	}
	if queue.PostingCount() != 1 {
		t.Fatalf("remaining queue postings = %d, want 1", queue.PostingCount())
	}
}

func TestOutboundDistributorConfirmsSentChunk(t *testing.T) {
	t.Parallel()

	word := yagomodel.WordHash("word")
	chunk := []yagomodel.RWIPosting{
		queuePosting(word, yagomodel.WordHash("url-a")),
	}
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), chunk)
	confirmer := &sentPostingConfirmerScript{confirmed: 1}

	receipt, err := NewConfirmingOutboundDistributor(
		queue,
		&capacityScript{count: 1},
		&handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)},
		confirmer,
	).Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("Distribute: %v", err)
	}

	if receipt.State != DistributionSent || receipt.ConfirmedPostings != 1 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if confirmer.calls != 1 || !reflect.DeepEqual(confirmer.postings, chunk) {
		t.Fatalf("confirmer = calls %d postings %#v", confirmer.calls, confirmer.postings)
	}
	if queue.PostingCount() != 0 {
		t.Fatalf("queue postings = %d, want sent chunk removed", queue.PostingCount())
	}
}

func TestOutboundDistributorConfirmationErrorDoesNotRequeueSentChunk(t *testing.T) {
	t.Parallel()

	word := yagomodel.WordHash("word")
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{
		queuePosting(word, yagomodel.WordHash("url-a")),
	})
	confirmErr := errors.New("confirm failed")

	receipt, err := NewConfirmingOutboundDistributor(
		queue,
		&capacityScript{count: 1},
		&handoffScript{receipt: acceptedHandoff(indextransfer.HandoffURLSent)},
		&sentPostingConfirmerScript{err: confirmErr},
	).Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if !errors.Is(err, confirmErr) {
		t.Fatalf("Distribute error = %v, want %v", err, confirmErr)
	}
	if receipt.State != DistributionSent || receipt.RequeuedPostings != 0 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.PostingCount() != 0 {
		t.Fatalf(
			"queue postings = %d, want no requeue after accepted handoff",
			queue.PostingCount(),
		)
	}
}

func TestOutboundDistributorRequeuesWhenCapacityProbeFails(t *testing.T) {
	t.Parallel()

	word := yagomodel.WordHash("word")
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{
		queuePosting(word, yagomodel.WordHash("url-a")),
		queuePosting(word, yagomodel.WordHash("url-b")),
	})
	probe := &capacityScript{err: errors.New("probe boom")}
	handoff := &handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)}

	receipt, err := NewOutboundDistributor(queue, probe, handoff).
		Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if err == nil {
		t.Fatal("expected capacity probe error")
	}

	if receipt.State != DistributionCapacityFailed || receipt.RequeuedPostings != 2 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.PostingCount() != 2 || handoff.calls != 0 {
		t.Fatalf(
			"queue/handoff = %d/%d, want requeued/no handoff",
			queue.PostingCount(),
			handoff.calls,
		)
	}
}

func TestOutboundDistributorRequeuesWhenHandoffFails(t *testing.T) {
	t.Parallel()

	word := yagomodel.WordHash("word")
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{
		queuePosting(word, yagomodel.WordHash("url-a")),
	})
	probe := &capacityScript{count: 1}
	handoff := &handoffScript{err: errors.New("handoff boom")}

	receipt, err := NewOutboundDistributor(queue, probe, handoff).
		Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if err == nil {
		t.Fatal("expected handoff error")
	}

	if receipt.State != DistributionHandoffFailed || receipt.RequeuedPostings != 1 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.PostingCount() != 1 {
		t.Fatalf("queue postings = %d, want requeued", queue.PostingCount())
	}
}

func TestOutboundDistributorRequeuesRejectedHandoff(t *testing.T) {
	t.Parallel()

	word := yagomodel.WordHash("word")
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{
		queuePosting(word, yagomodel.WordHash("url-a")),
	})
	probe := &capacityScript{count: 1}
	handoff := &handoffScript{
		receipt: indextransfer.HandoffReceipt{
			State: indextransfer.HandoffRWIRejected,
		},
	}

	receipt, err := NewOutboundDistributor(queue, probe, handoff).
		Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("Distribute: %v", err)
	}

	if receipt.State != DistributionHandoffRejected ||
		receipt.Handoff.State != indextransfer.HandoffRWIRejected ||
		receipt.RequeuedPostings != 1 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.PostingCount() != 1 {
		t.Fatalf("queue postings = %d, want requeued", queue.PostingCount())
	}
}

func acceptedHandoff(state indextransfer.HandoffState) indextransfer.HandoffReceipt {
	return indextransfer.HandoffReceipt{
		State: state,
	}
}
