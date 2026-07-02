package dhtexchange

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/indextransfer"
)

type capacityScript struct {
	count int
	err   error
	calls int
	peer  yacymodel.Hash
}

func (s *capacityScript) RWICount(
	_ context.Context,
	peer yacymodel.Seed,
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
	peer     yacymodel.Hash
	postings []yacymodel.RWIPosting
}

func (s *handoffScript) Send(
	_ context.Context,
	peer yacymodel.Seed,
	postings []yacymodel.RWIPosting,
) (indextransfer.HandoffReceipt, error) {
	s.calls++
	s.peer = peer.Hash
	s.postings = append([]yacymodel.RWIPosting(nil), postings...)
	if s.err != nil {
		return indextransfer.HandoffReceipt{}, s.err
	}

	return s.receipt, nil
}

func TestOutboundDistributorStopsWhenGatesAreClosed(t *testing.T) {
	t.Parallel()

	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yacymodel.RWIPosting{
		queuePosting(yacymodel.WordHash("word"), yacymodel.WordHash("url")),
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

	word := yacymodel.WordHash("word")
	largest := []yacymodel.RWIPosting{
		queuePosting(word, yacymodel.WordHash("url-a")),
		queuePosting(word, yacymodel.WordHash("url-b")),
	}
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "BBBBBBBBBBBB"), []yacymodel.RWIPosting{
		queuePosting(word, yacymodel.WordHash("url-c")),
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

func TestOutboundDistributorRequeuesWhenCapacityProbeFails(t *testing.T) {
	t.Parallel()

	word := yacymodel.WordHash("word")
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yacymodel.RWIPosting{
		queuePosting(word, yacymodel.WordHash("url-a")),
		queuePosting(word, yacymodel.WordHash("url-b")),
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

	word := yacymodel.WordHash("word")
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yacymodel.RWIPosting{
		queuePosting(word, yacymodel.WordHash("url-a")),
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

	word := yacymodel.WordHash("word")
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yacymodel.RWIPosting{
		queuePosting(word, yacymodel.WordHash("url-a")),
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
