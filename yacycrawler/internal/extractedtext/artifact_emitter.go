package extractedtext

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/docidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageparse"
)

const msgPageTextOverLimit = "extracted text page over size limit"

type ArtifactPublisher interface {
	Publish(ctx context.Context, text yacycrawlcontract.ExtractedText) error
}

type ArtifactEmitter interface {
	Emit(ctx context.Context, page pageparse.ParsedPage, crawledAt time.Time) error
}

type artifactEmitter struct {
	publisher      ArtifactPublisher
	trackingParams []string
	maxTextBytes   int
}

func NewArtifactEmitter(
	publisher ArtifactPublisher,
	trackingParams []string,
	maxTextBytes int,
) ArtifactEmitter {
	return &artifactEmitter{
		publisher:      publisher,
		trackingParams: trackingParams,
		maxTextBytes:   maxTextBytes,
	}
}

func (e *artifactEmitter) Emit(ctx context.Context, page pageparse.ParsedPage, crawledAt time.Time) error {
	canonical, ok := docidentity.CanonicalizeURL(page.URL, e.trackingParams)
	if !ok {
		return fmt.Errorf("canonicalize url: %s", page.URL)
	}
	if len(page.Text) > e.maxTextBytes {
		slog.WarnContext(ctx, msgPageTextOverLimit,
			slog.String("url", canonical),
			slog.Int("bytes", len(page.Text)),
			slog.Int("limit", e.maxTextBytes),
		)
		return nil
	}
	text := yacycrawlcontract.ExtractedText{
		CanonicalURL: canonical,
		DocumentID:   docidentity.DocumentID(canonical),
		Title:        page.Title,
		Text:         page.Text,
		CrawledAt:    crawledAt,
		Language:     page.Language,
	}
	if err := e.publisher.Publish(ctx, text); err != nil {
		return fmt.Errorf("publish extracted text %s: %w", canonical, err)
	}
	return nil
}
