package textsubscription

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	msgArtifactDecodeFailed = "extracted text artifact decode failed"
	msgArtifactIndexFailed  = "extracted text artifact index failed"
	msgArtifactIndexed      = "extracted text artifact indexed"
)

type Indexer interface {
	Index(ctx context.Context, text yacycrawlcontract.ExtractedText) error
}

type MessageSource interface {
	Messages(...jetstream.PullMessagesOpt) (jetstream.MessagesContext, error)
}

type ArtifactConsumer struct {
	consumer    MessageSource
	indexer     Indexer
	concurrency int
}

func NewArtifactConsumer(consumer MessageSource, indexer Indexer, concurrency int) *ArtifactConsumer {
	return &ArtifactConsumer{consumer: consumer, indexer: indexer, concurrency: concurrency}
}

func (c *ArtifactConsumer) Run(ctx context.Context) error {
	iter, err := c.consumer.Messages()
	if err != nil {
		return fmt.Errorf("open extracted text message iterator: %w", err)
	}
	defer iter.Stop()

	stopOnCancel := make(chan struct{})
	defer close(stopOnCancel)
	go func() {
		select {
		case <-ctx.Done():
			iter.Stop()
		case <-stopOnCancel:
		}
	}()

	var group sync.WaitGroup
	slots := make(chan struct{}, c.concurrency)
	for {
		msg, err := iter.Next()
		if err != nil {
			group.Wait()
			if errors.Is(err, jetstream.ErrMsgIteratorClosed) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("next extracted text message: %w", err)
		}
		slots <- struct{}{}
		group.Add(1)
		go func(msg jetstream.Msg) {
			defer group.Done()
			defer func() { <-slots }()
			c.processOne(ctx, msg)
		}(msg)
	}
}

func (c *ArtifactConsumer) processOne(ctx context.Context, msg jetstream.Msg) {
	text, err := yacycrawlcontract.UnmarshalExtractedText(msg.Data())
	if err != nil {
		slog.WarnContext(ctx, msgArtifactDecodeFailed, slog.Any("error", err))
		_ = msg.Term()
		return
	}
	if err := c.indexer.Index(ctx, text); err != nil {
		slog.WarnContext(ctx, msgArtifactIndexFailed,
			slog.String("url", text.CanonicalURL),
			slog.Any("error", err),
		)
		_ = msg.Nak()
		return
	}
	slog.DebugContext(ctx, msgArtifactIndexed, slog.String("url", text.CanonicalURL))
	_ = msg.Ack()
}
