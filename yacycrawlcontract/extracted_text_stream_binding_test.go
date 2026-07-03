package yacycrawlcontract_test

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

func TestEnsureExtractedTextStreamCreatesBoundedStream(t *testing.T) {
	js := connectJetStream(t, startNATS(t))

	spec := yacycrawlcontract.ExtractedTextStreamSpec{
		Subject: "yacy.crawl.extracted-text",
		MaxMsgs: 8,
	}
	if err := yacycrawlcontract.EnsureExtractedTextStream(context.Background(), js, spec); err != nil {
		t.Fatalf("ensure extracted text stream: %v", err)
	}

	stream, err := js.Stream(context.Background(), yacycrawlcontract.ExtractedTextStreamName)
	if err != nil {
		t.Fatalf("extracted text stream: %v", err)
	}
	cfg := stream.CachedInfo().Config
	if cfg.MaxMsgs != spec.MaxMsgs {
		t.Fatalf("MaxMsgs = %d, want %d", cfg.MaxMsgs, spec.MaxMsgs)
	}
	if cfg.Retention != jetstream.WorkQueuePolicy {
		t.Fatalf("retention = %v, want WorkQueuePolicy", cfg.Retention)
	}
	if cfg.Discard != jetstream.DiscardNew {
		t.Fatalf("discard = %v, want DiscardNew", cfg.Discard)
	}
}

func TestEnsureExtractedTextStreamReportsBrokerFailure(t *testing.T) {
	url := startNATS(t)
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("init jetstream: %v", err)
	}
	nc.Close()

	spec := yacycrawlcontract.ExtractedTextStreamSpec{Subject: "yacy.crawl.extracted-text", MaxMsgs: 8}
	if err := yacycrawlcontract.EnsureExtractedTextStream(context.Background(), js, spec); err == nil {
		t.Fatal("ensure extracted text stream on closed connection should fail")
	}
}
