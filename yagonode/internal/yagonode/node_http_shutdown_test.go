package yagonode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestShutdownStartsEveryServerDrainConcurrently(t *testing.T) {
	restoreMainSeams(t)
	first := buildServer("127.0.0.1:0", http.NotFoundHandler())
	second := buildServer("127.0.0.1:0", http.NotFoundHandler())
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	shutdownHTTPServer = func(server *http.Server, _ context.Context) error {
		if server == first {
			close(firstStarted)
			<-releaseFirst

			return nil
		}
		close(secondStarted)

		return nil
	}
	finished := make(chan error, 1)
	go func() {
		finished <- shutdown([]namedServer{{"first", first}, {"second", second}})
	}()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first server shutdown did not start")
	}
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second server waited behind the first shutdown")
	}
	close(releaseFirst)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent shutdown did not finish")
	}
}

func TestShutdownForcesConnectionsClosedAndJoinsHandlers(t *testing.T) {
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
	shutdownFailure := errors.New("shutdown deadline")
	closeFailure := errors.New("forced close failed")
	shutdownHTTPServer = func(*http.Server, context.Context) error {
		return shutdownFailure
	}
	forced := make(chan struct{})
	closeHTTPServer = func(*http.Server) error {
		close(forced)

		return closeFailure
	}
	finished := make(chan error, 1)
	go func() { finished <- shutdown([]namedServer{{"public", server}}) }()
	<-forced
	rejected := httptest.NewRecorder()
	server.Handler.ServeHTTP(
		rejected,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil),
	)
	if rejected.Code != http.StatusServiceUnavailable {
		t.Fatalf("request admitted during shutdown with status %d", rejected.Code)
	}
	select {
	case err := <-finished:
		t.Fatalf("shutdown returned before its handler: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(releaseHandler)
	<-requestFinished
	select {
	case err := <-finished:
		if !errors.Is(err, shutdownFailure) || !errors.Is(err, closeFailure) {
			t.Fatalf("shutdown error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not join the released handler")
	}
}

func TestShutdownAcceptsUnmanagedServer(t *testing.T) {
	restoreMainSeams(t)
	shutdownHTTPServer = func(*http.Server, context.Context) error { return nil }
	err := shutdown([]namedServer{{
		name: "external",
		server: &http.Server{
			Handler:           http.NotFoundHandler(),
			ReadHeaderTimeout: serverReadHeaderTimeout,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
}
