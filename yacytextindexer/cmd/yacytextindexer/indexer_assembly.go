package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/elasticsearchindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/pageintake"
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
	if err := yacycrawlcontract.EnsureCrawledPageStream(
		ctx,
		js,
		cfg.CrawledPageStreamSpec(),
	); err != nil {
		return fmt.Errorf("ensure crawled page stream: %w", err)
	}

	stream, err := js.Stream(ctx, yacycrawlcontract.CrawledPageStreamName)
	if err != nil {
		return fmt.Errorf("lookup crawled page stream: %w", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       cfg.CrawledPageDurable,
		FilterSubject: cfg.CrawledPageSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxAckPending: cfg.Concurrency,
	})
	if err != nil {
		return fmt.Errorf("create crawled page consumer: %w", err)
	}

	index := elasticsearchindex.NewElasticsearchIndex(
		cfg.ElasticsearchURL,
		cfg.ElasticsearchIndex,
		http.DefaultClient,
	)
	intake := pageintake.NewCrawledPageConsumer(consumer, index, cfg.Concurrency)

	slog.InfoContext(ctx, "textindexer started",
		slog.String("subject", cfg.CrawledPageSubject),
		slog.String("index", cfg.ElasticsearchIndex),
		slog.Int("concurrency", cfg.Concurrency),
	)
	if err := intake.Run(ctx); err != nil {
		return fmt.Errorf("run crawled page consumer: %w", err)
	}
	slog.InfoContext(ctx, "textindexer stopped")
	return nil
}
