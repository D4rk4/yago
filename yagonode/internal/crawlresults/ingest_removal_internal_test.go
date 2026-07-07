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
