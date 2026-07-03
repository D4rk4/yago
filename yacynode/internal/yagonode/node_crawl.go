package yagonode

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
	"github.com/D4rk4/yago/yacynode/internal/vault"
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
	config crawlConfig,
	identity nodeidentity.Identity,
	storage nodeStorage,
	storageVault *vault.Vault,
) (*crawlRuntime, error) {
	if !config.Enabled() {
		return nil, nil
	}

	broker, err := openCrawlBroker(crawlbroker.Config{ListenAddr: config.ListenAddr}, storageVault)
	if err != nil {
		return nil, fmt.Errorf("open crawl broker: %w", err)
	}

	consumer := crawlresults.NewIngestConsumerWithIndex(
		broker.Ingest,
		storage.documentReceiver,
		storage.searchIndex,
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
