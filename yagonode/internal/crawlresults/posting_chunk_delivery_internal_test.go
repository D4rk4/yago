package crawlresults

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
)

type postingChunkReceiver struct {
	sizes    []int
	receipts []rwi.Receipt
	errors   []error
}

func (r *postingChunkReceiver) Receive(
	_ context.Context,
	postings []yagomodel.RWIPosting,
) (rwi.Receipt, error) {
	call := len(r.sizes)
	r.sizes = append(r.sizes, len(postings))
	if call < len(r.errors) && r.errors[call] != nil {
		return rwi.Receipt{}, r.errors[call]
	}
	if call < len(r.receipts) {
		return r.receipts[call], nil
	}

	return rwi.Receipt{}, nil
}

func TestReceivePostingChunksBoundsEveryStorageTransaction(t *testing.T) {
	receiver := &postingChunkReceiver{receipts: []rwi.Receipt{
		{Pause: 2, UnknownURL: []yagomodel.Hash{"FirstHash001"}},
		{Pause: 4, UnknownURL: []yagomodel.Hash{"SecondHash02"}},
		{},
	}}
	postings := make([]yagomodel.RWIPosting, 2*yagocrawlcontract.MaximumIngestPostings+1)

	receipt, err := receivePostingChunks(t.Context(), receiver, postings)
	if err != nil {
		t.Fatalf("receivePostingChunks: %v", err)
	}
	if !slices.Equal(receiver.sizes, []int{
		yagocrawlcontract.MaximumIngestPostings,
		yagocrawlcontract.MaximumIngestPostings,
		1,
	}) {
		t.Fatalf("chunk sizes = %v", receiver.sizes)
	}
	if receipt.Pause != 4 || !slices.Equal(
		receipt.UnknownURL,
		[]yagomodel.Hash{"FirstHash001", "SecondHash02"},
	) {
		t.Fatalf("combined receipt = %+v", receipt)
	}
}

func TestReceivePostingChunksStopsAtBackpressure(t *testing.T) {
	receiver := &postingChunkReceiver{receipts: []rwi.Receipt{
		{},
		{Busy: true, Pause: 7},
	}}
	postings := make([]yagomodel.RWIPosting, 3*yagocrawlcontract.MaximumIngestPostings)

	receipt, err := receivePostingChunks(t.Context(), receiver, postings)
	if err != nil {
		t.Fatalf("receivePostingChunks: %v", err)
	}
	if !receipt.Busy || receipt.Pause != 7 || len(receiver.sizes) != 2 {
		t.Fatalf("receipt/calls = %+v/%v", receipt, receiver.sizes)
	}
}

func TestReceivePostingChunksStopsAtStorageFailure(t *testing.T) {
	want := errors.New("store failed")
	receiver := &postingChunkReceiver{errors: []error{nil, want}}
	postings := make([]yagomodel.RWIPosting, 3*yagocrawlcontract.MaximumIngestPostings)

	if _, err := receivePostingChunks(t.Context(), receiver, postings); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if len(receiver.sizes) != 2 {
		t.Fatalf("calls = %v, want two", receiver.sizes)
	}
}

func TestReceivePostingChunksStopsAtCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	receiver := &postingChunkReceiver{}

	if _, err := receivePostingChunks(
		ctx,
		receiver,
		make([]yagomodel.RWIPosting, 1),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
	if len(receiver.sizes) != 0 {
		t.Fatalf("calls after cancellation = %v", receiver.sizes)
	}
}

func TestReceivePostingChunksAcceptsEmptyGroup(t *testing.T) {
	receiver := &postingChunkReceiver{}

	receipt, err := receivePostingChunks(t.Context(), receiver, nil)
	if err != nil || receipt.Busy || receipt.Pause != 0 ||
		len(receipt.UnknownURL) != 0 || !slices.Equal(receiver.sizes, []int{0}) {
		t.Fatalf("receipt/error/calls = %+v/%v/%v", receipt, err, receiver.sizes)
	}
}

func TestReceivePostingChunksReportsEmptyStorageFailure(t *testing.T) {
	want := errors.New("empty store failed")
	receiver := &postingChunkReceiver{errors: []error{want}}

	if _, err := receivePostingChunks(t.Context(), receiver, nil); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
