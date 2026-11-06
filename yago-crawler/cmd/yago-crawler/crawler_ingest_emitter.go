package main

import "github.com/D4rk4/yago/yago-crawler/internal/ingest"

func newCrawlerIngestEmitter(
	nodeRPC crawlerNodeRPC,
	checkpoint crawlerCheckpointSession,
) ingest.BatchEmitter {
	return ingest.NewBatchEmitter(newCrawlerIngestPublisher(nodeRPC, checkpoint))
}

func newCrawlerNotModifiedEmitter(
	nodeRPC crawlerNodeRPC,
	checkpoint crawlerCheckpointSession,
) ingest.NotModifiedEmitter {
	return ingest.NewNotModifiedEmitter(newCrawlerIngestPublisher(nodeRPC, checkpoint))
}

func newCrawlerIngestPublisher(
	nodeRPC crawlerNodeRPC,
	checkpoint crawlerCheckpointSession,
) *ingest.GRPCIngestPublisher {
	return ingest.NewGRPCIngestPublisher(
		nodeRPC.ingest,
		ingest.WithIngestLeaseSession(
			checkpoint.workerID,
			checkpoint.workerSessionID,
			checkpoint.leaseGrants,
		),
	)
}
