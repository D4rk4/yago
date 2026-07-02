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

	"github.com/D4rk4/yago/yacycrawlcontract"
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

var (
	connectNATS        = nats.Connect
	closeNATS          = func(conn *nats.Conn) { conn.Close() }
	newJetStream       = jetstream.New
	ensureCrawlStreams = yacycrawlcontract.EnsureStreams
	openIngestReceiver = newIngestReceiver
)

func Open(ctx context.Context, cfg Config) (*CrawlBroker, error) {
	conn, err := connectNATS(cfg.NATSURL)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}

	js, err := newJetStream(conn)
	if err != nil {
		closeNATS(conn)
		return nil, fmt.Errorf("init jetstream: %w", err)
	}

	if err := ensureCrawlStreams(ctx, js, yacycrawlcontract.StreamSpec{
		OrdersSubject: cfg.OrdersSubject,
		IngestSubject: cfg.IngestSubject,
		IngestMaxMsgs: cfg.IngestMaxMsgs,
	}); err != nil {
		closeNATS(conn)
		return nil, fmt.Errorf("ensure streams: %w", err)
	}

	ingest, err := openIngestReceiver(ctx, js, cfg.IngestDurable, cfg.IngestSubject)
	if err != nil {
		closeNATS(conn)
		return nil, err
	}

	return &CrawlBroker{
		conn:   conn,
		Orders: newOrderPublisher(js, cfg.OrdersSubject),
		Ingest: ingest,
	}, nil
}

func (b *CrawlBroker) Close() {
	closeNATS(b.conn)
}
