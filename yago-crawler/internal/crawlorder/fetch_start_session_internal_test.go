package crawlorder

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type recordingFetchStartSession struct {
	mutex  sync.Mutex
	events []string
	stop   context.CancelFunc
}

func (session *recordingFetchStartSession) Connected() {
	session.record("connected")
}

func (session *recordingFetchStartSession) Disconnected() {
	session.record("disconnected")
}

func (session *recordingFetchStartSession) record(event string) {
	session.mutex.Lock()
	session.events = append(session.events, event)
	stop := session.stop
	shouldStop := len(session.events) == 3
	session.mutex.Unlock()
	if shouldStop {
		stop()
	}
}

func (session *recordingFetchStartSession) snapshot() []string {
	session.mutex.Lock()
	defer session.mutex.Unlock()

	return append([]string(nil), session.events...)
}

func TestOrderStreamSignalsFetchStartLeaseSession(t *testing.T) {
	fastOrderRetries(t)
	ctx, cancel := context.WithCancel(t.Context())
	session := &recordingFetchStartSession{stop: cancel}
	client := &fakeStreamer{
		ctx: ctx,
		attempts: []streamAttempt{
			{err: errors.New("stream unavailable")},
			{results: []recvResult{{err: io.EOF}}},
		},
	}
	receiver := NewGRPCOrderReceiver(
		ctx,
		client,
		"worker-fetch-start",
		nil,
		WithFetchStartSession(session),
	)
	drainUntilClosed(t, receiver)
	if events := session.snapshot(); !reflect.DeepEqual(events, []string{
		"disconnected",
		"connected",
		"disconnected",
	}) {
		t.Fatalf("fetch-start session events = %v", events)
	}
	client.mu.Lock()
	registrations := append([]*crawlrpc.WorkerRegistration(nil), client.registrations...)
	client.mu.Unlock()
	if len(registrations) != 2 {
		t.Fatalf("stream registrations = %d, want 2", len(registrations))
	}
	for _, registration := range registrations {
		if !registration.GetFetchStartLeases() {
			t.Fatalf("registration omitted fetch-start capability: %+v", registration)
		}
	}
}

func fastOrderRetries(t *testing.T) {
	t.Helper()
	restore := orderStreamRetryWait
	t.Cleanup(func() { orderStreamRetryWait = restore })
	orderStreamRetryWait = time.Millisecond
}
