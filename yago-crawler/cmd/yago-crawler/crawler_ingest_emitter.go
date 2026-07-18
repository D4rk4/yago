package main

import "github.com/D4rk4/yago/yago-crawler/internal/ingest"

func newCrawlerIngestEmitter(
	nodeRPC crawlerNodeRPC,
	checkpoint crawlerCheckpointSession,
) ingest.BatchEmitter {
	return ingest.NewBatchEmitter(ingest.NewGRPCIngestPublisher(
		nodeRPC.ingest,
		ingest.WithIngestLeaseSession(
			checkpoint.workerID,
			checkpoint.workerSessionID,
			checkpoint.leaseGrants,
		),
	))
}
