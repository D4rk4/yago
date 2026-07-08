// Package crawlorderbroker is the visit intake's NATS JetStream edge to the
// crawl fleet. It is the only place that speaks the broker protocol: it opens
// the connection, ensures the orders stream exists, and exposes OrderPlacement
// as the plain port the visit intake places orders through. Open wires the
// connection; Close releases it.
package crawlorderbroker

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
}

type CrawlOrderBroker struct {
	conn   *nats.Conn
	Orders *OrderPlacement
}

func Open(ctx context.Context, cfg Config) (*CrawlOrderBroker, error) {
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

	return &CrawlOrderBroker{
		conn:   conn,
		Orders: newOrderPlacement(js, cfg.OrdersSubject),
	}, nil
}

func (b *CrawlOrderBroker) Close() {
	b.conn.Close()
}
