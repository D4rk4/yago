package dhtexchange

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
)

type confirmationOutcome struct {
	confirmed int
	err       error
}

type confirmationOutcomeScript struct {
	outcomes []confirmationOutcome
	calls    [][]yagomodel.RWIPosting
}

func (s *confirmationOutcomeScript) ConfirmTransferred(
	_ context.Context,
	postings []yagomodel.RWIPosting,
) (int, error) {
	s.calls = append(s.calls, append([]yagomodel.RWIPosting(nil), postings...))
	outcome := s.outcomes[0]
	s.outcomes = s.outcomes[1:]

	return outcome.confirmed, outcome.err
}

func TestOutboundDistributorCancelsWholeRejectedRedundancyBeforeRestore(t *testing.T) {
	posting := queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url"))
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{posting})
	queue.add(queueSeed(t, "BBBBBBBBBBBB"), []yagomodel.RWIPosting{posting})
	handoff := &handoffScript{receipt: indextransfer.HandoffReceipt{
		State: indextransfer.HandoffRWIRejected,
	}}
	distributor := NewOutboundDistributor(
		queue,
		handoff,
		&wordRestorerScript{restored: 1},
	)

	first, err := distributor.Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if err != nil || first.RestoredPostings != 1 || queue.PostingCount() != 0 ||
		handoff.calls != 1 {
		t.Fatalf(
			"first receipt/error/queue/handoff = %#v/%v/%d/%d",
			first,
			err,
			queue.PostingCount(),
			handoff.calls,
		)
	}
	handoff.receipt = acceptedHandoff(indextransfer.HandoffRWIOnly)
	second, err := distributor.Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if err != nil || second.State != DistributionQueueEmpty || handoff.calls != 1 ||
		queue.PostingCount() != 0 {
		t.Fatalf(
			"second receipt/error/queue/handoff = %#v/%v/%d/%d",
			second,
			err,
			queue.PostingCount(),
			handoff.calls,
		)
	}
}

func TestOutboundDistributorCancelsRedundancyBeforeQuarantineRestore(t *testing.T) {
	t.Parallel()

	firstPeer := queueSeed(t, "AAAAAAAAAAAA")
	secondPeer := queueSeed(t, "BBBBBBBBBBBB")
	posting := queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url-a"))
	queue := NewOutboundQueue()
	queue.add(firstPeer, []yagomodel.RWIPosting{posting})
	queue.add(secondPeer, []yagomodel.RWIPosting{posting})
	restorer := &wordRestorerScript{restored: 1}
	distributor := NewOutboundDistributor(queue, &handoffScript{}, restorer)

	restored, requeued, err := distributor.RestoreRequeuedPeer(t.Context(), firstPeer.Hash)
	if err != nil || restored != 1 || requeued != 0 {
		t.Fatalf("restore = %d/%d/%v, want 1/0/nil", restored, requeued, err)
	}
	if queue.PostingCount() != 0 {
		t.Fatalf("queued sibling postings = %d, want 0", queue.PostingCount())
	}
}

func TestOutboundDistributorAcceptsAlreadyAbsentJournalRowsWithoutResending(t *testing.T) {
	firstPosting := queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url-a"))
	secondPosting := queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url-b"))
	postings := []yagomodel.RWIPosting{firstPosting, secondPosting}
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), postings)
	handoff := &handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)}
	confirmErr := errors.New("local confirmation failed")
	confirmer := &confirmationOutcomeScript{outcomes: []confirmationOutcome{
		{confirmed: 1, err: confirmErr},
		{},
	}}
	distributor := NewConfirmingOutboundDistributor(
		queue,
		handoff,
		&wordRestorerScript{},
		confirmer,
	)

	first, err := distributor.Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if !errors.Is(err, confirmErr) || first.State != DistributionSent ||
		first.ConfirmedPostings != 0 || handoff.calls != 1 || queue.PostingCount() != 0 ||
		!reflect.DeepEqual(queue.pendingTransferConfirmation(), postings) {
		t.Fatalf(
			"first receipt/error/handoff/queue/pending = %#v/%v/%d/%d/%#v",
			first,
			err,
			handoff.calls,
			queue.PostingCount(),
			queue.pendingTransferConfirmation(),
		)
	}
	second, err := distributor.Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if err != nil || second.State != DistributionConfirmed || second.Peer != "" ||
		second.ConfirmedPostings != 0 || handoff.calls != 1 ||
		len(queue.pendingTransferConfirmation()) != 0 || len(confirmer.calls) != 2 {
		t.Fatalf(
			"second receipt/error/handoff/pending/confirmer = %#v/%v/%d/%d/%#v",
			second,
			err,
			handoff.calls,
			len(queue.pendingTransferConfirmation()),
			confirmer,
		)
	}
}

func TestOutboundDistributorRetriesRejectedRestoreWithoutResending(t *testing.T) {
	posting := queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url"))
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{posting})
	handoff := &handoffScript{receipt: indextransfer.HandoffReceipt{
		State:            indextransfer.HandoffRWIOnly,
		RejectedPostings: []yagomodel.RWIPosting{posting},
	}}
	restoreErr := errors.New("local restore failed")
	restorer := &wordRestorerScript{err: restoreErr}
	distributor := NewOutboundDistributor(queue, handoff, restorer)

	first, err := distributor.Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if !errors.Is(err, restoreErr) || first.State != DistributionSent ||
		queue.PostingCount() != 0 || len(queue.pendingRestore()) != 1 || handoff.calls != 1 {
		t.Fatalf(
			"first receipt/error/queue/pending/handoff = %#v/%v/%d/%d/%d",
			first,
			err,
			queue.PostingCount(),
			len(queue.pendingRestore()),
			handoff.calls,
		)
	}
	second, err := distributor.Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if !errors.Is(err, restoreErr) || second.State != DistributionRestorePending ||
		second.Peer != "" || handoff.calls != 1 || len(queue.pendingRestore()) != 1 {
		t.Fatalf(
			"second receipt/error/pending/handoff = %#v/%v/%d/%d",
			second,
			err,
			len(queue.pendingRestore()),
			handoff.calls,
		)
	}
	restorer.err = nil
	restorer.restored = 0
	handoff.receipt = acceptedHandoff(indextransfer.HandoffRWIOnly)
	third, err := distributor.Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if err != nil || third.State != DistributionRestored || third.Peer != "" ||
		third.RestoredPostings != 0 || handoff.calls != 1 ||
		queue.PostingCount() != 0 || len(queue.pendingRestore()) != 0 {
		t.Fatalf(
			"third receipt/error/queue/pending/handoff = %#v/%v/%d/%d/%d",
			third,
			err,
			queue.PostingCount(),
			len(queue.pendingRestore()),
			handoff.calls,
		)
	}
}

func TestOutboundSchedulerDoesNotAttributeLocalConfirmationRetryToPeer(t *testing.T) {
	posting := queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url"))
	peer := queueSeed(t, "AAAAAAAAAAAA")
	queue := NewOutboundQueue()
	queue.add(peer, []yagomodel.RWIPosting{posting})
	handoff := &handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)}
	confirmErr := errors.New("local confirmation failed")
	confirmer := &confirmationOutcomeScript{outcomes: []confirmationOutcome{
		{err: confirmErr},
		{err: confirmErr},
		{confirmed: 1},
	}}
	retry := NewOutboundRetryPolicy(OutboundRetryConfig{})
	feed := &feedScript{receipt: OutboundFeedReceipt{State: OutboundFeedEmpty}}
	scheduler := NewOutboundScheduler(
		NewConfirmingOutboundDistributor(
			queue,
			handoff,
			&wordRestorerScript{},
			confirmer,
		),
		retry,
		&observedDistributions{},
		func(context.Context) GateState { return openGateState() },
		OutboundSchedulerConfig{Gates: DefaultGateConfig(), Feed: feed},
	)

	first, err := scheduler.RunOnce(t.Context())
	if !errors.Is(err, confirmErr) || first.Distribution.State != DistributionSent ||
		first.Retry.Status != OutboundRetryCleared || handoff.calls != 1 || feed.calls != 1 {
		t.Fatalf(
			"first receipt/error/handoff/feed = %#v/%v/%d/%d",
			first,
			err,
			handoff.calls,
			feed.calls,
		)
	}
	feed.err = errors.New("feed must wait for local completion")
	second, err := scheduler.RunOnce(t.Context())
	if !errors.Is(err, confirmErr) ||
		second.Distribution.State != DistributionConfirmationPending ||
		second.Distribution.Peer != "" || second.Retry.Status != OutboundRetryIgnored ||
		handoff.calls != 1 || feed.calls != 1 {
		t.Fatalf(
			"second receipt/error/handoff/feed = %#v/%v/%d/%d",
			second,
			err,
			handoff.calls,
			feed.calls,
		)
	}
	if _, attributed := retry.PeerState(peer.Hash); attributed {
		t.Fatal("local confirmation retry was attributed to the remote peer")
	}
	third, err := scheduler.RunOnce(t.Context())
	if err != nil || third.Distribution.State != DistributionConfirmed ||
		third.Distribution.ConfirmedPostings != 1 ||
		third.Retry.Status != OutboundRetryIgnored || handoff.calls != 1 || feed.calls != 1 {
		t.Fatalf(
			"third receipt/error/handoff/feed = %#v/%v/%d/%d",
			third,
			err,
			handoff.calls,
			feed.calls,
		)
	}
}
