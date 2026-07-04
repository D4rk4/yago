package crawledpage

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
	msgCrawledPageDecodeFailed = "crawled page decode failed"
	msgCrawledPageIndexFailed  = "crawled page index failed"
	msgCrawledPageIndexed      = "crawled page indexed"
)

type SearchIndex interface {
	Index(ctx context.Context, text yacycrawlcontract.CrawledPage) error
}

type CrawledPageMessages interface {
	Messages(...jetstream.PullMessagesOpt) (jetstream.MessagesContext, error)
}

type CrawledPageConsumer struct {
	consumer    CrawledPageMessages
	indexer     SearchIndex
	concurrency int
}

func NewCrawledPageConsumer(
	consumer CrawledPageMessages,
	indexer SearchIndex,
	concurrency int,
) *CrawledPageConsumer {
	return &CrawledPageConsumer{consumer: consumer, indexer: indexer, concurrency: concurrency}
}

func (c *CrawledPageConsumer) Run(ctx context.Context) error {
	iter, err := c.consumer.Messages()
	if err != nil {
		return fmt.Errorf("open crawled page message iterator: %w", err)
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
			return fmt.Errorf("next crawled page message: %w", err)
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

func (c *CrawledPageConsumer) processOne(ctx context.Context, msg jetstream.Msg) {
	text, err := yacycrawlcontract.UnmarshalCrawledPage(msg.Data())
	if err != nil {
		slog.WarnContext(ctx, msgCrawledPageDecodeFailed, slog.Any("error", err))
		_ = msg.Term()
		return
	}
	if err := c.indexer.Index(ctx, text); err != nil {
		slog.WarnContext(ctx, msgCrawledPageIndexFailed,
			slog.String("url", text.CanonicalURL),
			slog.Any("error", err),
		)
		_ = msg.Nak()
		return
	}
	slog.DebugContext(ctx, msgCrawledPageIndexed, slog.String("url", text.CanonicalURL))
	_ = msg.Ack()
}
