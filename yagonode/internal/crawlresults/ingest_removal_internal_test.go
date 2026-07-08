package crawlresults

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

type stubStream struct {
	out chan IngestDelivery
}

func (s stubStream) Receive() <-chan IngestDelivery { return s.out }

type countingPurger struct {
	calls int
}

func (p *countingPurger) Purge(context.Context, []yagomodel.Hash) error {
	p.calls++

	return nil
}

// TestAbsorbRemovalRejectsUnhashableURL covers the defensive branch where a
// tombstone's source URL cannot be hashed: HashURL never fails for a non-empty
// URL in practice, so the failure is injected here. The batch must be dropped
// (acked) rather than redelivered forever, and it must not purge.
func TestAbsorbRemovalRejectsUnhashableURL(t *testing.T) {
	purger := &countingPurger{}
	out := make(chan IngestDelivery, 1)
	settled := make(chan bool, 1)
	out <- IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{SourceURL: "https://a.example/1", Removed: true},
		Ack:   func(context.Context) error { settled <- true; return nil },
		Nak:   func(context.Context) error { settled <- false; return nil },
	}
	consumer := NewIngestConsumer(stubStream{out: out}, nil, nil, nil)
	consumer.PurgeURLs(purger)
	consumer.hashURL = func(string) (yagomodel.URLHash, error) {
		return "", errors.New("cannot hash")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	if acked := <-settled; !acked {
		t.Fatal("an unhashable removal URL must be dropped (acked), not redelivered")
	}
	if purger.calls != 0 {
		t.Fatalf("an unhashable removal must not purge, calls = %d", purger.calls)
	}
}

type recordingSweeper struct {
	url  yagomodel.Hash
	live map[yagomodel.Hash]struct{}
	call int
	err  error
}

func (s *recordingSweeper) PurgeStalePostings(
	_ context.Context,
	url yagomodel.Hash,
	live map[yagomodel.Hash]struct{},
) (int, error) {
	s.call++
	s.url = url
	s.live = live

	return len(live), s.err
}

func TestSweepStalePassesTheBatchWordSet(t *testing.T) {
	consumer := NewIngestConsumer(stubStream{}, nil, nil, nil)
	sweeper := &recordingSweeper{}
	consumer.SweepStalePostings(sweeper)

	batch := yagocrawlcontract.IngestBatch{
		SourceURL: "https://a.example/page",
		Postings: []yagomodel.RWIPosting{
			{WordHash: yagomodel.WordHash("alpha")},
			{WordHash: yagomodel.WordHash("beta")},
			{WordHash: yagomodel.WordHash("alpha")},
		},
	}
	if err := consumer.sweepStale(context.Background(), batch); err != nil {
		t.Fatalf("sweepStale: %v", err)
	}
	if sweeper.call != 1 || len(sweeper.live) != 2 {
		t.Fatalf("sweeper got %d calls, live=%v", sweeper.call, sweeper.live)
	}
	want, err := yagomodel.HashURL(batch.SourceURL)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if sweeper.url != want.Hash() {
		t.Fatalf("sweeper url = %v, want %v", sweeper.url, want.Hash())
	}
	if _, ok := sweeper.live[yagomodel.WordHash("alpha")]; !ok {
		t.Fatal("live set must carry the batch's word hashes")
	}
}

func TestSweepStaleSkipsPostingFreeBatches(t *testing.T) {
	consumer := NewIngestConsumer(stubStream{}, nil, nil, nil)
	sweeper := &recordingSweeper{}
	consumer.SweepStalePostings(sweeper)

	if err := consumer.sweepStale(
		context.Background(),
		yagocrawlcontract.IngestBatch{SourceURL: "https://a.example/empty"},
	); err != nil {
		t.Fatalf("sweepStale: %v", err)
	}
	if sweeper.call != 0 {
		t.Fatal("a batch without postings must not sweep")
	}
	consumer.SweepStalePostings(nil)
	if consumer.stale != sweeper {
		t.Fatal("a nil sweeper must keep the previous one")
	}
}

func TestSweepStaleSurfacesFailures(t *testing.T) {
	consumer := NewIngestConsumer(stubStream{}, nil, nil, nil)
	sweeper := &recordingSweeper{err: errors.New("sweep failed")}
	consumer.SweepStalePostings(sweeper)
	batch := yagocrawlcontract.IngestBatch{
		SourceURL: "https://a.example/page",
		Postings:  []yagomodel.RWIPosting{{WordHash: yagomodel.WordHash("alpha")}},
	}
	if err := consumer.sweepStale(context.Background(), batch); err == nil {
		t.Fatal("sweeper failure must surface")
	}

	consumer.hashURL = func(string) (yagomodel.URLHash, error) {
		return yagomodel.URLHash(""), errors.New("hash failed")
	}
	if err := consumer.sweepStale(context.Background(), batch); err == nil {
		t.Fatal("hash failure must surface")
	}
}
