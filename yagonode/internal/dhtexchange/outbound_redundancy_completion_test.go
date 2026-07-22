package dhtexchange

import (
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
)

func TestOutboundDistributorConfirmsOnlyAfterFinalRedundancyCopy(t *testing.T) {
	t.Parallel()

	posting := queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("url"))
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{posting})
	queue.add(queueSeed(t, "BBBBBBBBBBBB"), []yagomodel.RWIPosting{posting})
	confirmer := &sentPostingConfirmerScript{confirmed: 1}
	distributor := NewConfirmingOutboundDistributor(
		queue,
		&handoffScript{receipt: acceptedHandoff(indextransfer.HandoffRWIOnly)},
		&wordRestorerScript{},
		confirmer,
	)

	first, err := distributor.Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("first Distribute: %v", err)
	}
	if first.ConfirmedPostings != 0 || confirmer.calls != 0 || queue.PostingCount() != 1 {
		t.Fatalf(
			"first receipt/confirmer/queue = %#v/%#v/%d",
			first,
			confirmer,
			queue.PostingCount(),
		)
	}

	second, err := distributor.Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("second Distribute: %v", err)
	}
	if second.ConfirmedPostings != 1 || confirmer.calls != 1 ||
		!reflect.DeepEqual(confirmer.postings, []yagomodel.RWIPosting{posting}) ||
		queue.PostingCount() != 0 {
		t.Fatalf(
			"second receipt/confirmer/queue = %#v/%#v/%d",
			second,
			confirmer,
			queue.PostingCount(),
		)
	}
}

func TestOutboundDistributorRestoresOnlyErrorURLPostings(t *testing.T) {
	t.Parallel()

	word := yagomodel.WordHash("word")
	accepted := queuePosting(word, yagomodel.WordHash("accepted"))
	rejected := queuePosting(word, yagomodel.WordHash("rejected"))
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{accepted, rejected})
	restorer := &wordRestorerScript{restored: 1}
	confirmer := &sentPostingConfirmerScript{confirmed: 1}

	receipt, err := NewConfirmingOutboundDistributor(
		queue,
		&handoffScript{receipt: indextransfer.HandoffReceipt{
			State:            indextransfer.HandoffURLSent,
			RejectedPostings: []yagomodel.RWIPosting{rejected},
		}},
		restorer,
		confirmer,
	).Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("Distribute: %v", err)
	}
	if receipt.State != DistributionSent || receipt.RestoredPostings != 1 ||
		receipt.ConfirmedPostings != 1 || receipt.RequeuedPostings != 0 ||
		len(restorer.words) != 1 ||
		!reflect.DeepEqual(restorer.words[0].Postings, []yagomodel.RWIPosting{rejected}) ||
		!reflect.DeepEqual(confirmer.postings, []yagomodel.RWIPosting{accepted}) {
		t.Fatalf("receipt/restorer/confirmer = %#v/%#v/%#v", receipt, restorer, confirmer)
	}
}

func TestOutboundDistributorCancelsRejectedRedundancyCopies(t *testing.T) {
	t.Parallel()

	word := yagomodel.WordHash("word")
	accepted := queuePosting(word, yagomodel.WordHash("accepted"))
	rejected := queuePosting(word, yagomodel.WordHash("rejected"))
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{accepted, rejected})
	queue.add(queueSeed(t, "BBBBBBBBBBBB"), []yagomodel.RWIPosting{accepted, rejected})
	handoff := &handoffScript{receipt: indextransfer.HandoffReceipt{
		State:            indextransfer.HandoffURLSent,
		RejectedPostings: []yagomodel.RWIPosting{rejected},
	}}
	restorer := &wordRestorerScript{restored: 1}
	confirmer := &sentPostingConfirmerScript{confirmed: 1}
	distributor := NewConfirmingOutboundDistributor(queue, handoff, restorer, confirmer)

	first, err := distributor.Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("first Distribute: %v", err)
	}
	if first.RestoredPostings != 1 || first.ConfirmedPostings != 0 ||
		queue.PostingCount() != 1 || confirmer.calls != 0 {
		t.Fatalf(
			"first receipt/queue/confirmer = %#v/%d/%#v",
			first,
			queue.PostingCount(),
			confirmer,
		)
	}

	handoff.receipt = acceptedHandoff(indextransfer.HandoffRWIOnly)
	second, err := distributor.Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if err != nil {
		t.Fatalf("second Distribute: %v", err)
	}
	if second.ConfirmedPostings != 1 || queue.PostingCount() != 0 ||
		!reflect.DeepEqual(confirmer.postings, []yagomodel.RWIPosting{accepted}) {
		t.Fatalf(
			"second receipt/queue/confirmer = %#v/%d/%#v",
			second,
			queue.PostingCount(),
			confirmer,
		)
	}
}

func TestOutboundDistributorCancelsEmptyRejectedRedundancyChunk(t *testing.T) {
	t.Parallel()

	posting := queuePosting(yagomodel.WordHash("word"), yagomodel.WordHash("rejected"))
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{posting})
	queue.add(queueSeed(t, "BBBBBBBBBBBB"), []yagomodel.RWIPosting{posting})

	receipt, err := NewConfirmingOutboundDistributor(
		queue,
		&handoffScript{receipt: indextransfer.HandoffReceipt{
			State:            indextransfer.HandoffRWIOnly,
			RejectedPostings: []yagomodel.RWIPosting{posting},
		}},
		&wordRestorerScript{restored: 1},
		&sentPostingConfirmerScript{},
	).Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if err != nil || receipt.RestoredPostings != 1 || receipt.ConfirmedPostings != 0 ||
		queue.PostingCount() != 0 {
		t.Fatalf("receipt/error/queue = %#v/%v/%d", receipt, err, queue.PostingCount())
	}
}

func TestOutboundDistributorKeepsRejectedPostingInLocalRestoreRetry(t *testing.T) {
	t.Parallel()

	word := yagomodel.WordHash("word")
	accepted := queuePosting(word, yagomodel.WordHash("accepted"))
	rejected := queuePosting(word, yagomodel.WordHash("rejected"))
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{accepted, rejected})
	restoreErr := errors.New("restore failed")
	confirmer := &sentPostingConfirmerScript{confirmed: 1}

	receipt, err := NewConfirmingOutboundDistributor(
		queue,
		&handoffScript{receipt: indextransfer.HandoffReceipt{
			State:            indextransfer.HandoffRWIOnly,
			RejectedPostings: []yagomodel.RWIPosting{rejected},
		}},
		&wordRestorerScript{err: restoreErr},
		confirmer,
	).Distribute(t.Context(), openGateState(), DefaultGateConfig())
	if !errors.Is(err, restoreErr) || receipt.RestoredPostings != 0 ||
		receipt.RequeuedPostings != 0 || receipt.ConfirmedPostings != 1 ||
		queue.PostingCount() != 0 || len(queue.pendingRestore()) != 1 ||
		!reflect.DeepEqual(confirmer.postings, []yagomodel.RWIPosting{accepted}) {
		t.Fatalf(
			"receipt/error/confirmer/queue/pending = %#v/%v/%#v/%d/%d",
			receipt,
			err,
			confirmer,
			queue.PostingCount(),
			len(queue.pendingRestore()),
		)
	}
}

func TestOutboundCompletionLeavesMalformedPostingsRecoverable(t *testing.T) {
	t.Parallel()

	malformedWord := yagomodel.RWIPosting{WordHash: yagomodel.Hash("bad")}
	malformedURL := yagomodel.RWIPosting{WordHash: yagomodel.WordHash("word")}
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yagomodel.RWIPosting{malformedURL})

	if got := queue.confirmablePostings(
		[]yagomodel.RWIPosting{malformedWord, malformedURL},
	); len(
		got,
	) != 0 {
		t.Fatalf("confirmable malformed postings = %#v", got)
	}
	accepted, rejected := splitHandoffPostings(
		[]yagomodel.RWIPosting{malformedWord},
		[]yagomodel.RWIPosting{malformedURL},
	)
	if len(accepted) != 1 || len(rejected) != 0 {
		t.Fatalf("accepted/rejected = %#v/%#v", accepted, rejected)
	}
}
