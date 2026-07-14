package yagonode

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestForcedShutdownCompletesRequestedRestart(t *testing.T) {
	restoreMainSeams(t)
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	server := buildServer("127.0.0.1:0", http.HandlerFunc(func(
		http.ResponseWriter,
		*http.Request,
	) {
		close(handlerStarted)
		<-releaseHandler
	}))
	requestFinished := make(chan struct{})
	go func() {
		server.Handler.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil),
		)
		close(requestFinished)
	}()
	<-handlerStarted
	shutdownHTTPServer = func(_ *http.Server, ctx context.Context) error {
		<-ctx.Done()

		return ctx.Err()
	}
	closeHTTPServer = func(*http.Server) error {
		close(releaseHandler)

		return nil
	}
	_, restart := newRestartController(context.Background())
	restart.Trigger()
	err := restart.Wrap(shutdownWithin(
		[]namedServer{{"peer protocol", server}},
		time.Millisecond,
		time.Second,
	))
	if !errors.Is(err, errRestartRequested) {
		t.Fatalf("restart shutdown = %v, want requested restart", err)
	}
	select {
	case <-requestFinished:
	case <-time.After(time.Second):
		t.Fatal("forced shutdown returned before the active request")
	}
}

func TestForcedShutdownBoundsStuckRequestDrain(t *testing.T) {
	restoreMainSeams(t)
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	server := buildServer("127.0.0.1:0", http.HandlerFunc(func(
		http.ResponseWriter,
		*http.Request,
	) {
		close(handlerStarted)
		<-releaseHandler
	}))
	requestFinished := make(chan struct{})
	go func() {
		server.Handler.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil),
		)
		close(requestFinished)
	}()
	<-handlerStarted
	shutdownHTTPServer = func(_ *http.Server, ctx context.Context) error {
		<-ctx.Done()

		return ctx.Err()
	}
	closeHTTPServer = func(*http.Server) error { return nil }
	finished := make(chan error, 1)
	go func() {
		finished <- shutdownWithin(
			[]namedServer{{"peer protocol", server}},
			time.Millisecond,
			time.Millisecond,
		)
	}()
	select {
	case err := <-finished:
		if !errors.Is(err, context.DeadlineExceeded) ||
			!strings.Contains(err.Error(), "drain peer protocol") {
			t.Fatalf("bounded shutdown error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stuck request extended the forced shutdown bound")
	}
	close(releaseHandler)
	<-requestFinished
	requests := server.Handler.(*httpRequestLifecycle)
	if err := waitForHTTPRequests(requests, time.Second); err != nil {
		t.Fatalf("released request drain: %v", err)
	}
}

func TestForcedShutdownAcceptsAlreadyClosedListener(t *testing.T) {
	err := resolveHTTPShutdown(
		"peer protocol",
		context.DeadlineExceeded,
		net.ErrClosed,
		nil,
	)
	if err != nil {
		t.Fatalf("already closed forced shutdown = %v", err)
	}
}

func TestUnexpectedShutdownErrorRemainsFailure(t *testing.T) {
	failure := errors.New("listener failure")
	err := resolveHTTPShutdown("peer protocol", failure, nil, nil)
	if !errors.Is(err, failure) {
		t.Fatalf("unexpected shutdown error = %v", err)
	}
}
