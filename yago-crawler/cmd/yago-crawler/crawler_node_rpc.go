package main

import (
	"fmt"
	"io"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type crawlerNodeRPC struct {
	control       crawlrpc.CrawlExchangeClient
	ingest        crawlrpc.CrawlExchangeClient
	controlCloser io.Closer
	ingestCloser  io.Closer
}

func openCrawlerNodeRPC(address string) (crawlerNodeRPC, error) {
	control, controlCloser, err := newCrawlerExchange(address)
	if err != nil {
		return crawlerNodeRPC{}, fmt.Errorf("dial node control rpc: %w", err)
	}
	ingestClient, ingestCloser, err := newCrawlerExchange(address)
	if err != nil {
		_ = controlCloser.Close()

		return crawlerNodeRPC{}, fmt.Errorf("dial node ingest rpc: %w", err)
	}

	return crawlerNodeRPC{
		control:       control,
		ingest:        ingestClient,
		controlCloser: controlCloser,
		ingestCloser:  ingestCloser,
	}, nil
}

func (connections crawlerNodeRPC) close() {
	_ = connections.ingestCloser.Close()
	_ = connections.controlCloser.Close()
}
