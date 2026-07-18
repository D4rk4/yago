package crawlbroker

import (
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type exchangeServer struct {
	crawlrpc.UnimplementedCrawlExchangeServer
	queue       *DurableOrderQueue
	ingest      chan<- crawlresults.IngestDelivery
	beginIngest func() func()
	progress    ProgressSink
	control     *ControlRegistry
	sessions    *workerSessionRegistry
}

func newExchangeServer(
	queue *DurableOrderQueue,
	ingest chan<- crawlresults.IngestDelivery,
	defaults ...crawlerControlDefaults,
) *exchangeServer {
	leaseTTL := DefaultLeaseTTL
	if queue != nil && queue.leaseTTL > 0 {
		leaseTTL = queue.leaseTTL
	}
	return &exchangeServer{
		queue:    queue,
		ingest:   ingest,
		progress: noopProgressSink{},
		control:  newControlRegistry(defaults...),
		sessions: newWorkerSessionRegistry(maximumWorkerSessions, leaseTTL),
	}
}
