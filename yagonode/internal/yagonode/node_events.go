package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/eventstore"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type runtimeObservability struct {
	endpoints    *metrics.HTTPEndpointMetrics
	eviction     *metrics.EvictionMetrics
	dhtOutbound  *metrics.DHTOutboundMetrics
	dhtInbound   *metrics.DHTInboundMetrics
	peer         *metrics.PeerMetrics
	search       *metrics.SearchMetrics
	crawl        *metrics.CrawlMetrics
	indexWrites  *metrics.SearchIndexWriteMetrics
	crawlRuns    *metrics.CrawlRunMetrics
	remoteCrawl  *metrics.RemoteCrawlMetrics
	saturation   *metrics.SaturationMetrics
	recorder     *events.Recorder
	persistence  *eventPersistence
	authObserver authObserverFanOut
}

// provisionObservability registers the runtime metric collectors, opens the
// durable event log, and fans node auth events into both metrics and the
// recorder so the operator console begins populated with what survived a restart.
func provisionObservability(
	ctx context.Context,
	storage *vault.Vault,
) (runtimeObservability, error) {
	endpoints := metrics.NewHTTPEndpointMetrics()
	metrics.NewStorageMetrics(endpoints.Registry(), storage)

	recorder := events.NewRecorder(events.DefaultCapacity)
	persistence, err := attachDurableEvents(ctx, storage, recorder)
	if err != nil {
		return runtimeObservability{}, fmt.Errorf("configure event log: %w", err)
	}
	authMetrics := metrics.NewAuthMetrics(endpoints.Registry())
	saturation := metrics.NewSaturationMetrics(endpoints.Registry())

	return runtimeObservability{
		endpoints:    endpoints,
		eviction:     metrics.NewEvictionMetrics(endpoints.Registry()),
		dhtOutbound:  metrics.NewDHTOutboundMetrics(endpoints.Registry()),
		dhtInbound:   metrics.NewDHTInboundMetrics(endpoints.Registry()),
		peer:         metrics.NewPeerMetrics(endpoints.Registry()),
		saturation:   saturation,
		search:       metrics.NewSearchMetrics(endpoints.Registry()),
		crawl:        metrics.NewCrawlMetrics(endpoints.Registry()),
		indexWrites:  metrics.NewSearchIndexWriteMetrics(endpoints.Registry()),
		crawlRuns:    metrics.NewCrawlRunMetrics(endpoints.Registry()),
		remoteCrawl:  metrics.NewRemoteCrawlMetrics(endpoints.Registry()),
		recorder:     recorder,
		persistence:  persistence,
		authObserver: authObserverFanOut{authMetrics, authEventObserver{recorder: recorder}},
	}, nil
}

const (
	eventPersistenceCapacity     = 256
	eventPersistenceShutdownWait = 5 * time.Second
	msgEventPersistenceFailed    = "persist event failed"
	msgEventPersistenceFull      = "event persistence queue full"
	msgEventPersistenceTimeout   = "event persistence shutdown grace elapsed"
)

type eventPersistence struct {
	appender eventAppender

	mu     sync.Mutex
	queue  chan events.Event
	closed bool
	done   chan struct{}
	cancel context.CancelFunc
}

type eventAppender interface {
	Append(context.Context, events.Event) error
}

func newEventPersistence(appender eventAppender) *eventPersistence {
	workerCtx, cancel := context.WithCancel(context.Background())
	persistence := &eventPersistence{
		appender: appender,
		queue:    make(chan events.Event, eventPersistenceCapacity),
		done:     make(chan struct{}),
		cancel:   cancel,
	}
	go persistence.run(workerCtx)

	return persistence
}

func (p *eventPersistence) Persist(event events.Event) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()

		return
	}
	select {
	case p.queue <- event:
		p.mu.Unlock()
	default:
		p.mu.Unlock()
		slog.WarnContext(context.Background(), msgEventPersistenceFull,
			slog.String("event", event.Name))
	}
}

func (p *eventPersistence) run(ctx context.Context) {
	defer close(p.done)
	for {
		select {
		case event, ok := <-p.queue:
			if !ok {
				return
			}
			if ctx.Err() != nil {
				return
			}
			if err := p.appender.Append(ctx, event); err != nil {
				slog.WarnContext(context.Background(), msgEventPersistenceFailed,
					slog.String("event", event.Name),
					slog.Any("error", err))
			}
		case <-ctx.Done():
			return
		}
	}
}

func (p *eventPersistence) Close(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.queue)
	}
	p.mu.Unlock()

	select {
	case <-p.done:
		p.cancel()

		return nil
	case <-ctx.Done():
		p.cancel()

		return fmt.Errorf("close event persistence: %w", ctx.Err())
	}
}

func attachDurableEvents(
	ctx context.Context,
	storage *vault.Vault,
	recorder *events.Recorder,
) (*eventPersistence, error) {
	store, err := eventstore.Open(ctx, storage)
	if err != nil {
		return nil, fmt.Errorf("open event store: %w", err)
	}
	history, err := store.Recent(ctx)
	if err != nil {
		return nil, fmt.Errorf("load event history: %w", err)
	}
	persistence := newEventPersistence(store)
	recorder.Attach(persistence, history)

	return persistence, nil
}

func closeEventPersistence(persistence *eventPersistence) {
	closeEventPersistenceWithin(persistence, eventPersistenceShutdownWait)
}

func closeEventPersistenceWithin(persistence *eventPersistence, wait time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := persistence.Close(ctx); err != nil {
		slog.WarnContext(context.Background(), msgEventPersistenceTimeout,
			slog.Duration("grace", wait),
			slog.Any("error", err))
	}
}
