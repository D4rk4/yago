package crawlresults_test

import (
	"context"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type qualityRejection struct {
	acked bool
	rule  string
}

// deliverGatedPage submits one page and reports how the consumer consumed it:
// through the plain acknowledgement, or through the rejection hook that tells
// the crawler the document was not stored.
func deliverGatedPage(
	t *testing.T,
	stream *fakeStream,
	text string,
	withReject bool,
) qualityRejection {
	t.Helper()
	var (
		wg       sync.WaitGroup
		observed qualityRejection
	)
	wg.Add(1)
	delivery := crawlresults.IngestDelivery{
		Batch: yagocrawlcontract.IngestBatch{
			SourceURL: "https://spam.example/doorway",
			Document: yagocrawlcontract.DocumentIngest{
				NormalizedURL: "https://spam.example/doorway",
				ExtractedText: text,
			},
		},
		Ack: func(context.Context) error { observed.acked = true; wg.Done(); return nil },
		Nak: func(context.Context) error { wg.Done(); return nil },
	}
	if withReject {
		delivery.Reject = func(_ context.Context, rule string) error {
			observed.rule = rule
			wg.Done()

			return nil
		}
	}
	stream.out <- delivery
	wg.Wait()

	return observed
}

func gatedConsumer(t *testing.T, stream *fakeStream) *crawlresults.IngestConsumer {
	t.Helper()
	consumer := crawlresults.NewIngestConsumer(
		stream,
		&recordingDocumentReceiver{},
		&recordingURLReceiver{},
		&recordingPostingReceiver{},
	)
	consumer.GateQuality(func(text string) string {
		if len(text) < 100 {
			return "too-few-words"
		}

		return ""
	})

	return consumer
}

// A page the gate refuses must be consumed through Reject, naming the rule, so
// the crawler learns the document was not stored. Acknowledging it like a stored
// page is what made the crawler tally an unstored page as indexed.
func TestIngestGateRejectsRatherThanAcknowledges(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	consumer := gatedConsumer(t, stream)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	observed := deliverGatedPage(t, stream, "мало слов", true)

	if observed.acked {
		t.Fatal("a refused page was acknowledged as stored")
	}
	if observed.rule != "too-few-words" {
		t.Fatalf("rejection rule = %q, want the gate rule", observed.rule)
	}
}

// A delivery without the rejection hook falls back to a plain acknowledgement,
// so transports that do not distinguish the two keep working.
func TestIngestGateFallsBackToAcknowledgementWithoutRejectHook(t *testing.T) {
	stream := &fakeStream{out: make(chan crawlresults.IngestDelivery, 1)}
	consumer := gatedConsumer(t, stream)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	observed := deliverGatedPage(t, stream, "мало слов", false)

	if !observed.acked {
		t.Fatal("a refused page was neither rejected nor acknowledged")
	}
}
