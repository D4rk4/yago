package crawlbroker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacynode/internal/crawlresults"
)

const msgIngestDecodeFailed = "ingest batch decode failed"

type IngestReceiver struct {
	out chan crawlresults.IngestDelivery
}

type ingestConsumer interface {
	Consume(jetstream.MessageHandler, ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error)
}

var (
	createIngestConsumer = func(
		ctx context.Context,
		js jetstream.JetStream,
		durable string,
		subject string,
	) (ingestConsumer, error) {
		return js.CreateOrUpdateConsumer(
			ctx,
			yacycrawlcontract.IngestStreamName,
			jetstream.ConsumerConfig{
				Durable:       durable,
				AckPolicy:     jetstream.AckExplicitPolicy,
				FilterSubject: subject,
			},
		)
	}
	consumeIngestMessages = func(
		consumer ingestConsumer,
		handler jetstream.MessageHandler,
	) (jetstream.ConsumeContext, error) {
		return consumer.Consume(handler)
	}
)

func newIngestReceiver(
	ctx context.Context,
	js jetstream.JetStream,
	durable string,
	subject string,
) (*IngestReceiver, error) {
	consumer, err := createIngestConsumer(ctx, js, durable, subject)
	if err != nil {
		return nil, fmt.Errorf("create ingest consumer: %w", err)
	}

	out := make(chan crawlresults.IngestDelivery)
	consume, err := consumeIngestMessages(consumer, func(msg jetstream.Msg) {
		batch, err := yacycrawlcontract.UnmarshalIngestBatch(msg.Data())
		if err != nil {
			slog.WarnContext(context.Background(), msgIngestDecodeFailed, slog.Any("error", err))
			_ = msg.Term()
			return
		}
		delivery := crawlresults.IngestDelivery{
			Batch: batch,
			Ack:   func(context.Context) error { return msg.Ack() },
			Nak:   func(context.Context) error { return msg.Nak() },
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
