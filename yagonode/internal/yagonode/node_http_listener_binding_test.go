package yagonode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

type listenerReadyAnnouncer struct {
	bindings *atomic.Int32
	want     int32
	cancel   context.CancelFunc
	ready    chan bool
}

func (a listenerReadyAnnouncer) Run(context.Context) {
	a.ready <- a.bindings.Load() == a.want
	a.cancel()
}

func (listenerReadyAnnouncer) GreetDiscovered(context.Context, yagomodel.Seed) {}

type closingListener struct {
	closeErr error
	closed   atomic.Bool
}

func (l *closingListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }

func (l *closingListener) Close() error {
	l.closed.Store(true)

	return l.closeErr
}

func (*closingListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestServeBindsEveryListenerBeforeStartingAnnouncer(t *testing.T) {
	restoreMainSeams(t)
	ctx, cancel := context.WithCancel(t.Context())
	var bindings atomic.Int32
	bindHTTPListener = func(server *http.Server) (net.Listener, error) {
		var listenConfig net.ListenConfig
		listener, err := listenConfig.Listen(ctx, "tcp", server.Addr)
		if err != nil {
			return nil, fmt.Errorf("listen for binding test: %w", err)
		}
		bindings.Add(1)

		return listener, nil
	}
	ready := make(chan bool, 1)
	assembled := node{
		announcer: listenerReadyAnnouncer{
			bindings: &bindings, want: 2, cancel: cancel, ready: ready,
		},
		sweeper: &scriptedSweeper{},
	}
	err := serve(
		ctx,
		assembled,
		metrics.NewEvictionMetrics(prometheus.NewRegistry()),
		namedServer{"peer", buildServer("127.0.0.1:0", http.NotFoundHandler())},
		namedServer{"ops", buildServer("127.0.0.1:0", http.NotFoundHandler())},
	)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if prepared := <-ready; !prepared {
		t.Fatalf("announcer started after %d of 2 listener bindings", bindings.Load())
	}
}

func TestBindHTTPServersClosesEarlierListenersAfterFailure(t *testing.T) {
	restoreMainSeams(t)
	bindFailure := errors.New("bind failed")
	closeFailure := errors.New("close failed")
	first := &closingListener{closeErr: closeFailure}
	calls := 0
	bindHTTPListener = func(*http.Server) (net.Listener, error) {
		calls++
		if calls == 1 {
			return first, nil
		}

		return nil, bindFailure
	}
	bound, err := bindHTTPServers([]namedServer{
		{"peer", buildServer("127.0.0.1:0", http.NotFoundHandler())},
		{"ops", buildServer("127.0.0.1:0", http.NotFoundHandler())},
	})
	if bound != nil || !errors.Is(err, bindFailure) || !errors.Is(err, closeFailure) ||
		!first.closed.Load() {
		t.Fatalf("partial bind = %+v, %v, closed=%v", bound, err, first.closed.Load())
	}
}

func TestBindHTTPServersReturnsSuccessfulBindings(t *testing.T) {
	restoreMainSeams(t)
	listener := &closingListener{}
	bindHTTPListener = func(*http.Server) (net.Listener, error) { return listener, nil }
	bound, err := bindHTTPServers([]namedServer{{
		"peer", buildServer("127.0.0.1:0", http.NotFoundHandler()),
	}})
	if err != nil || len(bound) != 1 || bound[0].listener != listener {
		t.Fatalf("bindings = %+v, %v", bound, err)
	}
	if err := closeBoundHTTPServers(bound); err != nil || !listener.closed.Load() {
		t.Fatalf("close bindings = %v, closed=%v", err, listener.closed.Load())
	}
}
