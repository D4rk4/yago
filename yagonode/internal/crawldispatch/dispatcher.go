package crawldispatch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagomodel"
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
// with the local initiator and a freshly minted provenance token. It remembers
// the most recent request per profile handle so a finished or failed run can
// be restarted from the monitor without re-entering the form.
type Dispatcher struct {
	initiator      yagomodel.Hash
	mint           ProvenanceMint
	queue          CrawlOrderQueue
	now            func() time.Time
	maxPagesPerRun func() int

	mu           sync.Mutex
	lastByHandle map[string]OperatorRequest
}

// NewDispatcher builds a dispatcher over the given crawl order queue.
func NewDispatcher(
	initiator yagomodel.Hash,
	mint ProvenanceMint,
	queue CrawlOrderQueue,
	options ...DispatcherOption,
) *Dispatcher {
	dispatcher := &Dispatcher{
		initiator:    initiator,
		mint:         mint,
		queue:        queue,
		now:          time.Now,
		lastByHandle: map[string]OperatorRequest{},
	}
	for _, option := range options {
		option(dispatcher)
	}

	return dispatcher
}

// ErrNoRestartableOrder marks a restart for a profile this dispatcher never
// dispatched (or forgot across a restart of the node itself).
var ErrNoRestartableOrder = errors.New("no restartable crawl order for profile")

// Restart re-dispatches the most recent request seen for the profile handle
// under a fresh provenance and timestamp, producing a new run.
func (d *Dispatcher) Restart(ctx context.Context, profileHandle string) (Accepted, error) {
	d.mu.Lock()
	req, ok := d.lastByHandle[profileHandle]
	d.mu.Unlock()
	if !ok {
		return Accepted{}, &DispatchError{
			Err: fmt.Errorf("%w: %s", ErrNoRestartableOrder, profileHandle),
		}
	}

	return d.Dispatch(ctx, req, "")
}

// Dispatch builds the crawl order from req and enqueues it under key. A nil error
// means the order was accepted. The returned error is always a *DispatchError:
// Retryable true is a delivery failure, otherwise the request was invalid.
func (d *Dispatcher) Dispatch(
	ctx context.Context,
	req OperatorRequest,
	key string,
) (Accepted, error) {
	order, err := req.order(d.initiator, d.mint(), d.now(), d.MaxPagesPerRun())
	if err != nil {
		return Accepted{}, &DispatchError{Err: err}
	}

	d.mu.Lock()
	d.lastByHandle[order.Profile.Handle] = req
	d.mu.Unlock()

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
