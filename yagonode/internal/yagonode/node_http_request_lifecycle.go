package yagonode

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

type httpRequestLifecycle struct {
	state   sync.Mutex
	active  sync.WaitGroup
	stopped bool
	next    http.Handler
}

func newHTTPRequestLifecycle(next http.Handler) *httpRequestLifecycle {
	return &httpRequestLifecycle{next: next}
}

func (l *httpRequestLifecycle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !l.admit() {
		http.Error(w, "server shutting down", http.StatusServiceUnavailable)

		return
	}
	defer l.active.Done()
	l.next.ServeHTTP(w, r)
}

func (l *httpRequestLifecycle) admit() bool {
	l.state.Lock()
	defer l.state.Unlock()
	if l.stopped {
		return false
	}
	l.active.Add(1)

	return true
}

func (l *httpRequestLifecycle) stopAccepting() {
	l.state.Lock()
	l.stopped = true
	l.state.Unlock()
}

func (l *httpRequestLifecycle) wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		l.active.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for active HTTP requests: %w", ctx.Err())
	}
}
