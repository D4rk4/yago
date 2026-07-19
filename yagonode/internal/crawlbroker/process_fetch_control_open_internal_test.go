package crawlbroker

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func TestOpenNormalizesInvalidProcessFetchControls(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	broker, err := Open(Config{
		ListenAddr:            "127.0.0.1:0",
		ProcessPagesPerSecond: -1,
		MaximumRedirects:      yagocrawlcontract.MaximumPageRedirects + 1,
	}, storage, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(broker.Close)
	if got := broker.Control.ProcessPagesPerSecond(); got != yagocrawlcontract.DefaultProcessPagesPerSecond {
		t.Fatalf("process rate = %d, want %d", got,
			yagocrawlcontract.DefaultProcessPagesPerSecond)
	}
	if got := broker.Control.MaximumRedirects(); got != yagocrawlcontract.DefaultMaximumPageRedirects {
		t.Fatalf("maximum redirects = %d, want %d", got,
			yagocrawlcontract.DefaultMaximumPageRedirects)
	}
}

func TestOpenReportsFetchStartSchedulePreparationFailure(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	original := prepareCrawlExchange
	preparationError := errors.New("fetch-start preparation failed")
	prepareCrawlExchange = func(
		*DurableOrderQueue,
		chan<- crawlresults.IngestDelivery,
		...crawlerControlDefaults,
	) (*exchangeServer, error) {
		return nil, preparationError
	}
	t.Cleanup(func() { prepareCrawlExchange = original })

	broker, err := Open(Config{ListenAddr: "127.0.0.1:0"}, storage, nil)
	if broker != nil || !errors.Is(err, preparationError) {
		t.Fatalf("open result = (%v, %v), want preparation error", broker, err)
	}
}

func TestOpenReportsFetchStartAuthorityBindingFailure(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	original := prepareCrawlExchange
	prepareCrawlExchange = func(
		queue *DurableOrderQueue,
		ingest chan<- crawlresults.IngestDelivery,
		defaults ...crawlerControlDefaults,
	) (*exchangeServer, error) {
		exchange, err := newExchangeServerChecked(queue, ingest, defaults...)
		if err != nil {
			return nil, err
		}
		exchange.fetchStarts = nil

		return exchange, nil
	}
	t.Cleanup(func() { prepareCrawlExchange = original })

	broker, err := Open(Config{ListenAddr: "127.0.0.1:0"}, storage, nil)
	if broker != nil || !errors.Is(err, errFleetFetchPolicyInvalid) {
		t.Fatalf("open result = (%v, %v), want authority binding error", broker, err)
	}
}
