// Package crawlbroker is the node's NATS JetStream edge to the crawl fleet. It is
// the only place that speaks the broker protocol: it publishes crawl orders and
// receives ingest batches, exposing them as the plain ports the inner packages
// consume. Open wires the connection; Close releases it.
package crawlbroker

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type Config struct {
	NATSURL       string
	OrdersSubject string
	IngestSubject string
	IngestDurable string
	IngestMaxMsgs int64
}

type CrawlBroker struct {
	conn   *nats.Conn
	Orders *OrderPublisher
	Ingest *IngestReceiver
}

func Open(ctx context.Context, cfg Config) (*CrawlBroker, error) {
	conn, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("init jetstream: %w", err)
	}

	if err := yacycrawlcontract.EnsureOrdersStream(ctx, js, yacycrawlcontract.OrdersStreamSpec{
		Subject: cfg.OrdersSubject,
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ensure orders stream: %w", err)
	}
	if err := yacycrawlcontract.EnsureCrawledPageIndexStream(
		ctx,
		js,
		yacycrawlcontract.CrawledPageIndexStreamSpec{
			Subject: cfg.IngestSubject,
			MaxMsgs: cfg.IngestMaxMsgs,
		},
	); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ensure ingest stream: %w", err)
	}

	ingest, err := newIngestReceiver(ctx, js, cfg.IngestDurable, cfg.IngestSubject)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return &CrawlBroker{
		conn:   conn,
		Orders: newOrderPublisher(js, cfg.OrdersSubject),
		Ingest: ingest,
	}, nil
}

func (b *CrawlBroker) Close() {
	b.conn.Close()
}
