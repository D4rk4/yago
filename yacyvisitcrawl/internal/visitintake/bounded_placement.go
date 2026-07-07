package visitintake

import (
	"context"
	"log/slog"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	msgOrderPlaceSaturated = "crawl order place skipped: too many in flight"
	msgOrderPlaceFailed    = "crawl order place failed"
)

type BoundedPlacement struct {
	place    func(ctx context.Context, order yacycrawlcontract.CrawlOrder) error
	metrics  VisitMetrics
	timeout  time.Duration
	inFlight chan struct{}
}

func NewBoundedPlacement(
	place func(ctx context.Context, order yacycrawlcontract.CrawlOrder) error,
	metrics VisitMetrics,
	timeout time.Duration,
	maxInFlight int,
) *BoundedPlacement {
	return &BoundedPlacement{
		place:    place,
		metrics:  metrics,
		timeout:  timeout,
		inFlight: make(chan struct{}, maxInFlight),
	}
}

func (p *BoundedPlacement) Attempt(order yacycrawlcontract.CrawlOrder) {
	select {
	case p.inFlight <- struct{}{}:
	default:
		p.metrics.OrderUnplaced()
		slog.WarnContext(context.Background(), msgOrderPlaceSaturated,
			slog.String("order", order.OrderID))
		return
	}

	go func() {
		defer func() { <-p.inFlight }()

		ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
		defer cancel()

		if err := p.place(ctx, order); err != nil {
			p.metrics.OrderUnplaced()
			slog.WarnContext(ctx, msgOrderPlaceFailed,
				slog.String("order", order.OrderID), slog.Any("error", err))
			return
		}
		p.metrics.OrderPlaced()
	}()
}
