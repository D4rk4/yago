package crawlbroker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/crawlresults"
)

const (
	msgIngestDecodeFailed = "ingest batch decode failed"
	ingestNakDelay        = 5 * time.Second
)

type IngestReceiver struct {
	out chan crawlresults.IngestDelivery
}

func newIngestReceiver(
	ctx context.Context,
	js jetstream.JetStream,
	durable string,
	subject string,
) (*IngestReceiver, error) {
	consumer, err := js.CreateOrUpdateConsumer(
		ctx,
		yacycrawlcontract.CrawledPageIndexStreamName,
		jetstream.ConsumerConfig{
			Durable:       durable,
			AckPolicy:     jetstream.AckExplicitPolicy,
			FilterSubject: subject,
			MaxAckPending: 1,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create ingest consumer: %w", err)
	}

	out := make(chan crawlresults.IngestDelivery)
	consume, err := consumer.Consume(func(msg jetstream.Msg) {
		message, err := yacycrawlcontract.UnmarshalCrawledPageIndexMessage(msg.Data())
		if err != nil {
			slog.WarnContext(context.Background(), msgIngestDecodeFailed, slog.Any("error", err))
			_ = msg.Term()
			return
		}
		delivery := crawlresults.IngestDelivery{
			Message: message,
			Ack:     func(context.Context) error { return msg.Ack() },
			Nak:     func(context.Context) error { return msg.NakWithDelay(ingestNakDelay) },
		}
		select {
		case out <- delivery:
		case <-ctx.Done():
			_ = msg.Nak()
		}
	})
	if err != nil {
		return nil, fmt.Errorf("consume ingest: %w", err)
	}

	go func() {
		<-ctx.Done()
		consume.Stop()
	}()

	return &IngestReceiver{out: out}, nil
}

func (r *IngestReceiver) Receive() <-chan crawlresults.IngestDelivery {
	return r.out
}
