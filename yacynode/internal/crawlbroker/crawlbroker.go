// Package crawlbroker is the node's gRPC edge to the crawl fleet. It is the only
// place that speaks the CrawlExchange service: it serves a durable queue of
// crawl orders to crawler streams and receives ingest batches back, exposing
// them as the plain ports the inner packages consume. Open starts the server;
// Close stops it.
package crawlbroker

import (
	"fmt"
	"net"

	"google.golang.org/grpc"

	"github.com/D4rk4/yago/yacycrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type Config struct {
	ListenAddr string
}

type CrawlBroker struct {
	Orders   *DurableOrderQueue
	Ingest   *IngestReceiver
	server   *grpc.Server
	listener net.Listener
}

var (
	listenCrawlRPC = func(addr string) (net.Listener, error) {
		return net.Listen("tcp", addr)
	}
	// nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- internal node-crawler control plane on a trusted network; transport security is deferred (ADR-0014).
	newGRPCServer = func() *grpc.Server { return grpc.NewServer() }
)

func Open(cfg Config, storage *vault.Vault) (*CrawlBroker, error) {
	queue, err := newDurableOrderQueue(storage)
	if err != nil {
		return nil, err
	}
	listener, err := listenCrawlRPC(cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen crawl rpc %q: %w", cfg.ListenAddr, err)
	}

	ingest := newIngestReceiver()
	server := newGRPCServer()
	crawlrpc.RegisterCrawlExchangeServer(server, newExchangeServer(queue, ingest.out))
	go func() { _ = server.Serve(listener) }()

	return &CrawlBroker{Orders: queue, Ingest: ingest, server: server, listener: listener}, nil
}

func (b *CrawlBroker) Close() {
	b.server.Stop()
}
