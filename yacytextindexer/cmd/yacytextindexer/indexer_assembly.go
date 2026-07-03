package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/searchdocument"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/textsubscription"
)

func RunService(ctx context.Context, cfg ServiceConfig) error {
	nc, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("init jetstream: %w", err)
	}
	if err := yacycrawlcontract.EnsureExtractedTextStream(ctx, js, cfg.StreamSpec()); err != nil {
		return fmt.Errorf("ensure extracted text stream: %w", err)
	}

	stream, err := js.Stream(ctx, yacycrawlcontract.ExtractedTextStreamName)
	if err != nil {
		return fmt.Errorf("lookup extracted text stream: %w", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       cfg.Durable,
		FilterSubject: cfg.ExtractedTextSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxAckPending: cfg.Concurrency,
	})
	if err != nil {
		return fmt.Errorf("create extracted text consumer: %w", err)
	}

	index := searchdocument.NewElasticsearchIndex(cfg.ElasticsearchURL, cfg.ElasticsearchIndex, http.DefaultClient)
	artifacts := textsubscription.NewArtifactConsumer(consumer, index, cfg.Concurrency)

	slog.InfoContext(ctx, "textindexer started",
		slog.String("subject", cfg.ExtractedTextSubject),
		slog.String("index", cfg.ElasticsearchIndex),
		slog.Int("concurrency", cfg.Concurrency),
	)
	if err := artifacts.Run(ctx); err != nil {
		return fmt.Errorf("run artifact consumer: %w", err)
	}
	slog.InfoContext(ctx, "textindexer stopped")
	return nil
}
