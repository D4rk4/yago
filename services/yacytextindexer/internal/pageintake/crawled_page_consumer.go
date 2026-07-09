package pageintake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	msgCrawledPageDecodeFailed = "crawled page decode failed"
	msgCrawledPageIndexFailed  = "crawled page index failed"
	msgCrawledPageIndexed      = "crawled page indexed"

	disposalReasonUndecodable = "undecodable"
)

type SearchIndex interface {
	Index(ctx context.Context, page yacycrawlcontract.CrawledPage) error
}

type IndexProgress interface {
	PageReceived()
	PageIndexed()
	PageDisposed(reason string)
	IndexFailed()
	IndexObserved(elapsed time.Duration)
}

type CrawledPageSource interface {
	Messages(...jetstream.PullMessagesOpt) (jetstream.MessagesContext, error)
}

type CrawledPageConsumer struct {
	source      CrawledPageSource
	indexer     SearchIndex
	progress    IndexProgress
	concurrency int
}

func NewCrawledPageConsumer(
	source CrawledPageSource,
	indexer SearchIndex,
	progress IndexProgress,
	concurrency int,
) *CrawledPageConsumer {
	return &CrawledPageConsumer{
		source:      source,
		indexer:     indexer,
		progress:    progress,
		concurrency: concurrency,
	}
}

func (c *CrawledPageConsumer) Run(ctx context.Context) error {
	iter, err := c.source.Messages()
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
	c.progress.PageReceived()
	page, err := yacycrawlcontract.UnmarshalCrawledPage(msg.Data())
	if err != nil {
		slog.WarnContext(ctx, msgCrawledPageDecodeFailed, slog.Any("error", err))
		c.progress.PageDisposed(disposalReasonUndecodable)
		_ = msg.Term()
		return
	}
	started := time.Now()
	err = c.indexer.Index(ctx, page)
	c.progress.IndexObserved(time.Since(started))
	if err != nil {
		slog.WarnContext(ctx, msgCrawledPageIndexFailed,
			slog.String("url", page.CanonicalURL),
			slog.Any("error", err),
		)
		c.progress.IndexFailed()
		_ = msg.Nak()
		return
	}
	c.progress.PageIndexed()
	slog.DebugContext(ctx, msgCrawledPageIndexed, slog.String("url", page.CanonicalURL))
	_ = msg.Ack()
}
