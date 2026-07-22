package dhtexchange

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
)

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

	return s.receipt, s.err
}

type wordRestorerScript struct {
	restored int
	err      error
	calls    int
	words    []yagomodel.WordPostings
}

func (s *wordRestorerScript) RestoreOutboundWords(
	_ context.Context,
	words []yagomodel.WordPostings,
) (int, error) {
	s.calls++
	s.words = append([]yagomodel.WordPostings(nil), words...)
	if s.err != nil {
		return 0, s.err
	}

	return s.restored, nil
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
	handoff := &handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)}
	restorer := &wordRestorerScript{}
	state := openGateState()
	state.OnlineCaution = "proxy"

	receipt, err := NewOutboundDistributor(queue, handoff, restorer).
		Distribute(context.Background(), state, DefaultGateConfig())
	if err != nil {
		t.Fatalf("Distribute: %v", err)
	}

	if receipt.State != DistributionGateClosed ||
		receipt.Gates.BlockingReason != GateOnlineCautionReason {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.PostingCount() != 1 || handoff.calls != 0 || restorer.calls != 0 {
		t.Fatalf(
			"queue/handoff/restore = %d/%d/%d, want untouched",
			queue.PostingCount(),
			handoff.calls,
			restorer.calls,
		)
	}
}

func TestOutboundDistributorReportsEmptyQueue(t *testing.T) {
	t.Parallel()

	handoff := &handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)}
	restorer := &wordRestorerScript{}
	receipt, err := NewOutboundDistributor(NewOutboundQueue(), handoff, restorer).
		Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("Distribute: %v", err)
	}

	if receipt.State != DistributionQueueEmpty || handoff.calls != 0 || restorer.calls != 0 {
		t.Fatalf("receipt/handoff/restore = %#v/%d/%d", receipt, handoff.calls, restorer.calls)
	}
}

func TestOutboundDistributorSendsLargestChunkWithoutCapacityQuery(t *testing.T) {
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
	handoff := &handoffScript{receipt: acceptedHandoff(indextransfer.HandoffURLSent)}

	receipt, err := NewOutboundDistributor(queue, handoff, &wordRestorerScript{}).
		Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("Distribute: %v", err)
	}

	if receipt.State != DistributionSent ||
		receipt.Peer != queueHash(t, "AAAAAAAAAAAA") ||
		receipt.Target.Hash != queueHash(t, "AAAAAAAAAAAA") ||
		receipt.PostingCount != 2 ||
		receipt.Handoff.State != indextransfer.HandoffURLSent {
		t.Fatalf("receipt = %#v", receipt)
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
		&handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)},
		&wordRestorerScript{},
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
		&handoffScript{receipt: acceptedHandoff(indextransfer.HandoffURLSent)},
		&wordRestorerScript{},
		&sentPostingConfirmerScript{err: confirmErr},
	).Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if !errors.Is(err, confirmErr) {
		t.Fatalf("Distribute error = %v, want %v", err, confirmErr)
	}
	if receipt.State != DistributionSent || receipt.RequeuedPostings != 0 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.PostingCount() != 0 || len(queue.pendingTransferConfirmation()) != 1 {
		t.Fatalf(
			"queue/pending confirmation = %d/%d, want local-only confirmation",
			queue.PostingCount(),
			len(queue.pendingTransferConfirmation()),
		)
	}
}

func TestOutboundDistributorRequeuesTransportFailure(t *testing.T) {
	t.Parallel()

	word := yagomodel.WordHash("word")
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{
		queuePosting(word, yagomodel.WordHash("url-a")),
	})
	handoff := &handoffScript{err: errors.New("handoff boom")}
	restorer := &wordRestorerScript{}

	receipt, err := NewOutboundDistributor(queue, handoff, restorer).
		Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if err == nil {
		t.Fatal("expected handoff error")
	}

	if receipt.State != DistributionHandoffFailed || receipt.RequeuedPostings != 1 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.PostingCount() != 1 || restorer.calls != 0 {
		t.Fatalf(
			"queue/restore = %d/%d, want requeued without early restore",
			queue.PostingCount(),
			restorer.calls,
		)
	}
}

func TestOutboundDistributorRestoresRejectedHandoffForRetargeting(t *testing.T) {
	t.Parallel()

	firstWord := queueHash(t, "WWWWWWWWWWWW")
	secondWord := queueHash(t, "XXXXXXXXXXXX")
	postings := []yagomodel.RWIPosting{
		queuePosting(firstWord, yagomodel.WordHash("url-a")),
		queuePosting(secondWord, yagomodel.WordHash("url-b")),
	}
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), postings)
	restorer := &wordRestorerScript{restored: 2}
	handoff := &handoffScript{
		receipt: indextransfer.HandoffReceipt{State: indextransfer.HandoffRWIRejected},
	}

	receipt, err := NewOutboundDistributor(queue, handoff, restorer).
		Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("Distribute: %v", err)
	}

	if receipt.State != DistributionHandoffRejected ||
		receipt.RestoredPostings != 2 ||
		receipt.RequeuedPostings != 0 ||
		queue.PostingCount() != 0 ||
		restorer.calls != 1 ||
		len(restorer.words) != 2 ||
		restorer.words[0].WordHash != firstWord ||
		restorer.words[1].WordHash != secondWord {
		t.Fatalf("receipt/queue/restore = %#v/%d/%#v", receipt, queue.PostingCount(), restorer)
	}
}

func TestOutboundDistributorRetainsRejectedChunkForLocalRestoreRetry(t *testing.T) {
	t.Parallel()

	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{
		queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url-a")),
	})
	restoreErr := errors.New("restore failed")
	restorer := &wordRestorerScript{err: restoreErr}
	handoff := &handoffScript{
		receipt: indextransfer.HandoffReceipt{State: indextransfer.HandoffRWIRejected},
	}

	receipt, err := NewOutboundDistributor(queue, handoff, restorer).
		Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if !errors.Is(err, restoreErr) {
		t.Fatalf("Distribute error = %v, want %v", err, restoreErr)
	}
	if receipt.State != DistributionHandoffRejected ||
		receipt.RestoredPostings != 0 ||
		receipt.RequeuedPostings != 0 ||
		queue.PostingCount() != 0 ||
		len(queue.pendingRestore()) != 1 {
		t.Fatalf(
			"receipt/queue/pending = %#v/%d/%d",
			receipt,
			queue.PostingCount(),
			len(queue.pendingRestore()),
		)
	}
}

func TestOutboundDistributorRestoresRequeuedPeerAfterRetryLimit(t *testing.T) {
	t.Parallel()

	peer := queueSeed(t, "AAAAAAAAAAAA")
	queue := NewOutboundQueue()
	queue.add(peer, []yagomodel.RWIPosting{
		queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url-a")),
	})
	restorer := &wordRestorerScript{restored: 1}
	distributor := NewOutboundDistributor(queue, &handoffScript{}, restorer)

	restored, requeued, err := distributor.RestoreRequeuedPeer(context.Background(), peer.Hash)
	if err != nil || restored != 1 || requeued != 0 || queue.PostingCount() != 0 {
		t.Fatalf("restore = %d/%d/%v queue=%d", restored, requeued, err, queue.PostingCount())
	}
	restored, requeued, err = distributor.RestoreRequeuedPeer(context.Background(), peer.Hash)
	if err != nil || restored != 0 || requeued != 0 {
		t.Fatalf("empty restore = %d/%d/%v", restored, requeued, err)
	}
}

func TestOutboundDistributorRequeuesPeerWhenBoundedRestoreFails(t *testing.T) {
	t.Parallel()

	peer := queueSeed(t, "AAAAAAAAAAAA")
	queue := NewOutboundQueue()
	queue.add(peer, []yagomodel.RWIPosting{
		queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url-a")),
	})
	restoreErr := errors.New("restore failed")
	distributor := NewOutboundDistributor(
		queue,
		&handoffScript{},
		&wordRestorerScript{err: restoreErr},
	)

	restored, requeued, err := distributor.RestoreRequeuedPeer(context.Background(), peer.Hash)
	if !errors.Is(err, restoreErr) || restored != 0 || requeued != 1 || queue.PostingCount() != 1 {
		t.Fatalf("restore = %d/%d/%v queue=%d", restored, requeued, err, queue.PostingCount())
	}
}

func TestOutboundDistributorRestoresMalformedProtocolResponseImmediately(t *testing.T) {
	t.Parallel()

	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{
		queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url-a")),
	})
	protocolErr := errors.New("malformed response")
	restorer := &wordRestorerScript{restored: 1}

	receipt, err := NewOutboundDistributor(
		queue,
		&handoffScript{
			receipt: indextransfer.HandoffReceipt{State: indextransfer.HandoffRWIRejected},
			err:     protocolErr,
		},
		restorer,
	).Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if !errors.Is(err, protocolErr) ||
		receipt.State != DistributionHandoffRejected ||
		receipt.RestoredPostings != 1 ||
		receipt.RequeuedPostings != 0 ||
		queue.PostingCount() != 0 {
		t.Fatalf("receipt/error/queue = %#v/%v/%d", receipt, err, queue.PostingCount())
	}
}

func TestOutboundDistributorRetainsMalformedResponseWhenRestoreFails(t *testing.T) {
	t.Parallel()

	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{
		queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url-a")),
	})
	protocolErr := errors.New("malformed response")
	restoreErr := errors.New("restore failed")

	receipt, err := NewOutboundDistributor(
		queue,
		&handoffScript{
			receipt: indextransfer.HandoffReceipt{State: indextransfer.HandoffRWIRejected},
			err:     protocolErr,
		},
		&wordRestorerScript{err: restoreErr},
	).Distribute(context.Background(), openGateState(), DefaultGateConfig())
	if !errors.Is(err, protocolErr) ||
		!errors.Is(err, restoreErr) ||
		receipt.State != DistributionHandoffRejected ||
		receipt.RequeuedPostings != 0 ||
		queue.PostingCount() != 0 ||
		len(queue.pendingRestore()) != 1 {
		t.Fatalf(
			"receipt/error/queue/pending = %#v/%v/%d/%d",
			receipt,
			err,
			queue.PostingCount(),
			len(queue.pendingRestore()),
		)
	}
}

func acceptedHandoff(state indextransfer.HandoffState) indextransfer.HandoffReceipt {
	return indextransfer.HandoffReceipt{State: state}
}
