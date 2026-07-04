package crawldispatch

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

// Accepted describes a crawl order the dispatcher enqueued.
type Accepted struct {
	ProfileHandle string
	Seeds         int
	Duplicate     bool
}

// DispatchError distinguishes a rejected request (invalid input) from a delivery
// failure so callers can map each to the right status. Retryable is true only
// for a delivery failure.
type DispatchError struct {
	Err       error
	Retryable bool
}

func (e *DispatchError) Error() string { return e.Err.Error() }

func (e *DispatchError) Unwrap() error { return e.Err }

// Dispatcher turns an operator crawl request into a durable crawl order stamped
// with the local initiator and a freshly minted provenance token.
type Dispatcher struct {
	initiator yacymodel.Hash
	mint      ProvenanceMint
	queue     CrawlOrderQueue
	now       func() time.Time
}

// NewDispatcher builds a dispatcher over the given crawl order queue.
func NewDispatcher(
	initiator yacymodel.Hash,
	mint ProvenanceMint,
	queue CrawlOrderQueue,
) *Dispatcher {
	return &Dispatcher{initiator: initiator, mint: mint, queue: queue, now: time.Now}
}

// Dispatch builds the crawl order from req and enqueues it under key. A nil error
// means the order was accepted. The returned error is always a *DispatchError:
// Retryable true is a delivery failure, otherwise the request was invalid.
func (d *Dispatcher) Dispatch(
	ctx context.Context,
	req OperatorRequest,
	key string,
) (Accepted, error) {
	order, err := req.order(d.initiator, d.mint(), d.now())
	if err != nil {
		return Accepted{}, &DispatchError{Err: err}
	}

	duplicate, err := d.queue.PublishOnce(ctx, key, order)
	if err != nil {
		return Accepted{}, &DispatchError{
			Err:       fmt.Errorf("publish crawl order: %w", err),
			Retryable: true,
		}
	}

	return Accepted{
		ProfileHandle: order.Profile.Handle,
		Seeds:         len(order.Requests),
		Duplicate:     duplicate,
	}, nil
}
