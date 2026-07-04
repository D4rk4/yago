package crawledpage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/docidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageparse"
)

const msgCrawledPageOverLimit = "crawled page dropped over size limit"

type CrawledPagePublisher interface {
	Publish(ctx context.Context, text yacycrawlcontract.CrawledPage) error
}

type CrawledPageEmitter interface {
	Emit(ctx context.Context, page pageparse.ParsedPage, crawledAt time.Time) error
}

type crawledPageEmitter struct {
	publisher      CrawledPagePublisher
	trackingParams []string
}

func NewCrawledPageEmitter(
	publisher CrawledPagePublisher,
	trackingParams []string,
) CrawledPageEmitter {
	return &crawledPageEmitter{
		publisher:      publisher,
		trackingParams: trackingParams,
	}
}

func (e *crawledPageEmitter) Emit(
	ctx context.Context,
	page pageparse.ParsedPage,
	crawledAt time.Time,
) error {
	canonical, ok := docidentity.CanonicalizeURL(page.URL, e.trackingParams)
	if !ok {
		return fmt.Errorf("canonicalize url: %s", page.URL)
	}
	text := yacycrawlcontract.CrawledPage{
		CanonicalURL: canonical,
		DocumentID:   docidentity.DocumentID(canonical),
		Title:        page.Title,
		Text:         page.Text,
		CrawledAt:    crawledAt,
		Language:     page.Language,
	}
	if err := e.publisher.Publish(ctx, text); err != nil {
		if errors.Is(err, ErrCrawledPageOversized) {
			slog.WarnContext(ctx, msgCrawledPageOverLimit,
				slog.String("url", canonical),
				slog.Int("bytes", len(page.Text)),
			)
			return nil
		}
		return fmt.Errorf("publish crawled page %s: %w", canonical, err)
	}
	return nil
}
