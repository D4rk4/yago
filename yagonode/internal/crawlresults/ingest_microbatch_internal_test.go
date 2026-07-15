package crawlresults

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

// batchStaleSweeperStub offers both the per-URL and the whole-batch sweep, so a
// consumer treats it as a staleBatchSweeper and takes the grouped sweep path.
type batchStaleSweeperStub struct {
	batchCalls int
}

func (s *batchStaleSweeperStub) PurgeStalePostings(
	context.Context,
	yagomodel.Hash,
	map[yagomodel.Hash]struct{},
) (int, error) {
	return 0, nil
}

func (s *batchStaleSweeperStub) PurgeStalePostingsForURLs(
	context.Context,
	map[yagomodel.Hash]map[yagomodel.Hash]struct{},
) (int, error) {
	s.batchCalls++

	return 0, nil
}

// TestDrainPendingStopsWhenStreamCloses covers the drain loop's closed-stream
// exit: the first delivery is returned alone once Receive reports the stream is
// drained and shut.
func TestDrainPendingStopsWhenStreamCloses(t *testing.T) {
	out := make(chan IngestDelivery)
	close(out)
	consumer := NewIngestConsumer(stubStream{out: out}, nil, nil, nil)

	group := consumer.drainPending(t.Context(), IngestDelivery{})
	if len(group) != 1 {
		t.Fatalf("drainPending len = %d, want 1 (only the first delivery)", len(group))
	}
}

// TestDrainPendingStopsAtMicroBatchCap covers the drain loop's cap exit: with
// more than a full micro-batch already waiting, the group stops growing at the
// cap instead of draining the whole backlog.
func TestDrainPendingStopsAtMicroBatchCap(t *testing.T) {
	out := make(chan IngestDelivery, ingestMicroBatch)
	for range ingestMicroBatch - 1 {
		out <- IngestDelivery{}
	}
	consumer := NewIngestConsumer(stubStream{out: out}, nil, nil, nil)

	group := consumer.drainPending(t.Context(), IngestDelivery{})
	if len(group) != ingestMicroBatch {
		t.Fatalf("drainPending len = %d, want the micro-batch cap %d", len(group), ingestMicroBatch)
	}
}

// TestSweepStaleGroupRedeliversUnhashableURL covers the grouped sweep's
// defensive hash failure: HashURL never fails for a non-empty URL in practice,
// so the failure is injected. The delivery drops out of the group (naked) and
// no URL survives to sweep.
func TestSweepStaleGroupRedeliversUnhashableURL(t *testing.T) {
	consumer := NewIngestConsumer(stubStream{}, nil, nil, nil)
	sweeper := &batchStaleSweeperStub{}
	consumer.SweepStalePostings(sweeper)
	consumer.hashURL = func(string) (yagomodel.URLHash, error) {
		return "", errors.New("cannot hash")
	}

	naked := make(chan struct{}, 1)
	group := []IngestDelivery{{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://a.example/1",
			Postings:  []yagomodel.RWIPosting{{WordHash: yagomodel.WordHash("w")}},
		},
		Nak: func(context.Context) error { naked <- struct{}{}; return nil },
	}}

	kept := consumer.sweepStaleGroup(context.Background(), group)
	if len(kept) != 0 {
		t.Fatalf("kept = %d, want 0 (the unhashable delivery leaves the group)", len(kept))
	}
	if sweeper.batchCalls != 0 {
		t.Fatalf("batch sweep calls = %d, want 0 (no hashable URL to sweep)", sweeper.batchCalls)
	}
	if len(naked) != 1 {
		t.Fatal("an unhashable delivery must be redelivered (naked)")
	}
}
