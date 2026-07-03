package extractedtext_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/extractedtext"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageparse"
)

type recordingPublisher struct {
	published yacycrawlcontract.ExtractedText
	err       error
}

func (p *recordingPublisher) Publish(_ context.Context, text yacycrawlcontract.ExtractedText) error {
	p.published = text
	return p.err
}

func TestArtifactEmitterPublishesCanonicalizedArtifact(t *testing.T) {
	publisher := &recordingPublisher{}
	emitter := extractedtext.NewArtifactEmitter(publisher, nil, 1<<20)
	crawledAt := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	page := pageparse.ParsedPage{URL: "https://Example.com/", Title: "Hi", Text: "words here", Language: "en"}

	if err := emitter.Emit(context.Background(), page, crawledAt); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if publisher.published.CanonicalURL != "https://example.com/" {
		t.Errorf("canonical url = %q", publisher.published.CanonicalURL)
	}
	if publisher.published.DocumentID == "" {
		t.Error("expected non-empty document id")
	}
	if publisher.published.Title != "Hi" || publisher.published.Text != "words here" {
		t.Errorf("artifact = %+v", publisher.published)
	}
	if !publisher.published.CrawledAt.Equal(crawledAt) {
		t.Errorf("crawled at = %v", publisher.published.CrawledAt)
	}
}

func TestArtifactEmitterDropsOversizedPage(t *testing.T) {
	publisher := &recordingPublisher{}
	emitter := extractedtext.NewArtifactEmitter(publisher, nil, 4)
	page := pageparse.ParsedPage{URL: "https://example.com/", Text: "way too long"}

	if err := emitter.Emit(context.Background(), page, time.Now()); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if publisher.published.CanonicalURL != "" {
		t.Error("expected oversized page to be dropped, not published")
	}
}

func TestArtifactEmitterPropagatesPublishError(t *testing.T) {
	publisher := &recordingPublisher{err: errors.New("boom")}
	emitter := extractedtext.NewArtifactEmitter(publisher, nil, 1<<20)
	page := pageparse.ParsedPage{URL: "https://example.com/"}

	if err := emitter.Emit(context.Background(), page, time.Now()); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestNoopArtifactEmitterNeverErrors(t *testing.T) {
	emitter := extractedtext.NewNoopArtifactEmitter()
	if err := emitter.Emit(context.Background(), pageparse.ParsedPage{}, time.Now()); err != nil {
		t.Fatalf("noop emit: %v", err)
	}
}
