package crawlbroker

import (
	"sync"

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
	urlDenylist *crawlURLDenylistDelivery
	fetchStarts *fleetFetchStartSchedule
	fetchPolicy sync.Mutex
}

func newExchangeServer(
	queue *DurableOrderQueue,
	ingest chan<- crawlresults.IngestDelivery,
	defaults ...crawlerControlDefaults,
) *exchangeServer {
	server, err := newExchangeServerChecked(queue, ingest, defaults...)
	if err != nil {
		panic(err)
	}

	return server
}

func newExchangeServerChecked(
	queue *DurableOrderQueue,
	ingest chan<- crawlresults.IngestDelivery,
	defaults ...crawlerControlDefaults,
) (*exchangeServer, error) {
	leaseTTL := DefaultLeaseTTL
	if queue != nil && queue.leaseTTL > 0 {
		leaseTTL = queue.leaseTTL
	}
	pagesPerSecond := uint32(0)
	if len(defaults) > 0 && defaults[0].processRateSet {
		pagesPerSecond = defaults[0].processPagesPerSecond
	}
	fetchStarts, err := newFleetFetchStartSchedule(pagesPerSecond)
	if err != nil {
		return nil, err
	}
	server := &exchangeServer{
		queue:       queue,
		ingest:      ingest,
		progress:    noopProgressSink{},
		control:     newControlRegistry(defaults...),
		sessions:    newWorkerSessionRegistry(maximumWorkerSessions, leaseTTL),
		urlDenylist: newCrawlURLDenylistDelivery(),
		fetchStarts: fetchStarts,
	}
	server.control.bindFleetFetchStarts(server.setFleetPagesPerSecond)

	return server, nil
}
