package yagonode

import (
	"context"
	"fmt"
	"log/slog"

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
	recorder     *events.Recorder
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
	if err := attachDurableEvents(ctx, storage, recorder); err != nil {
		return runtimeObservability{}, fmt.Errorf("configure event log: %w", err)
	}
	authMetrics := metrics.NewAuthMetrics(endpoints.Registry())

	return runtimeObservability{
		endpoints:    endpoints,
		eviction:     metrics.NewEvictionMetrics(endpoints.Registry()),
		dhtOutbound:  metrics.NewDHTOutboundMetrics(endpoints.Registry()),
		dhtInbound:   metrics.NewDHTInboundMetrics(endpoints.Registry()),
		peer:         metrics.NewPeerMetrics(endpoints.Registry()),
		recorder:     recorder,
		authObserver: authObserverFanOut{authMetrics, authEventObserver{recorder: recorder}},
	}, nil
}

// eventSink write-through persists each recorded event to the durable event log.
// It is best-effort: a persistence failure is logged and never blocks recording.
type eventSink struct {
	store *eventstore.Store
}

func (s eventSink) Persist(event events.Event) {
	if err := s.store.Append(context.Background(), event); err != nil {
		slog.WarnContext(context.Background(), "persist event failed", slog.Any("error", err))
	}
}

// attachDurableEvents opens the durable event log, seeds the recorder with the
// events that survived the last restart, and installs a write-through sink so new
// events are persisted.
func attachDurableEvents(
	ctx context.Context,
	storage *vault.Vault,
	recorder *events.Recorder,
) error {
	store, err := eventstore.Open(ctx, storage)
	if err != nil {
		return fmt.Errorf("open event store: %w", err)
	}
	history, err := store.Recent(ctx)
	if err != nil {
		return fmt.Errorf("load event history: %w", err)
	}
	recorder.Attach(eventSink{store: store}, history)

	return nil
}
