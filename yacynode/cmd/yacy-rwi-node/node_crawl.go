package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/crawlbroker"
	"github.com/D4rk4/yago/yacynode/internal/crawldispatch"
	"github.com/D4rk4/yago/yacynode/internal/crawlresults"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
)

type crawlProcess interface {
	mountDispatch(mux *http.ServeMux)
	Run(ctx context.Context)
	Close()
}

type crawlRuntime struct {
	broker    *crawlbroker.CrawlBroker
	consumer  *crawlresults.IngestConsumer
	initiator yacymodel.Hash
}

var openCrawlBroker = crawlbroker.Open

func buildCrawlRuntime(
	ctx context.Context,
	config crawlConfig,
	identity nodeidentity.Identity,
	storage nodeStorage,
) (*crawlRuntime, error) {
	if !config.Enabled() {
		return nil, nil
	}

	broker, err := openCrawlBroker(ctx, crawlbroker.Config{
		NATSURL:       config.NATSURL,
		OrdersSubject: config.OrdersSubject,
		IngestSubject: config.IngestSubject,
		IngestDurable: config.IngestDurable,
		IngestMaxMsgs: config.IngestMaxMsgs,
	})
	if err != nil {
		return nil, fmt.Errorf("open crawl broker: %w", err)
	}

	consumer := crawlresults.NewIngestConsumer(
		broker.Ingest,
		storage.urlReceiver,
		storage.postingReceiver,
	)

	return &crawlRuntime{
		broker:    broker,
		consumer:  consumer,
		initiator: identity.Hash,
	}, nil
}

func (r *crawlRuntime) mountDispatch(mux *http.ServeMux) {
	crawldispatch.MountCrawlDispatch(mux, r.initiator, mintProvenance, r.broker.Orders)
}

func (r *crawlRuntime) Run(ctx context.Context) {
	r.consumer.Run(ctx)
}

func (r *crawlRuntime) Close() {
	r.broker.Close()
}

func mintProvenance() []byte {
	token := make([]byte, yacymodel.HashLength)
	_, _ = rand.Read(token)
	return token
}
