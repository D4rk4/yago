package firefoxfetch

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newSaturatedFirefoxPool(
	t *testing.T,
	observer *recordedBrowserPoolObservation,
) (*firefoxPool, chan struct{}, <-chan error) {
	t.Helper()
	started := make(chan struct{})
	release := make(chan struct{})
	pool := newFirefoxPoolObserved(
		BrowserLaunch{Sessions: 1},
		"http://proxy.example",
		func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return &fakeSession{
				aliveVal: true,
				renderFunc: func(
					context.Context,
					string,
					time.Duration,
				) (renderedPage, error) {
					close(started)
					<-release

					return renderedPage{url: "https://example.org/first"}, nil
				},
			}, nil
		},
		browserPoolObservation{observer: observer},
	)
	done := make(chan error, 1)
	go func() {
		_, err := pool.render(context.Background(), "https://example.org/first")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first browser render did not acquire the only slot")
	}

	return pool, release, done
}

func TestFirefoxPoolCountsBrowserSlotAcquisitionDeadline(t *testing.T) {
	observer := &recordedBrowserPoolObservation{}
	pool, release, first := newSaturatedFirefoxPool(t, observer)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := pool.render(ctx, "https://example.org/second")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second render error = %v, want context deadline", err)
	}
	if _, _, failures := observer.snapshot(); len(failures) != 1 ||
		failures[0] != BrowserFailureSlotDeadline {
		t.Fatalf("browser slot acquisition failures = %v, want deadline", failures)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first render: %v", err)
	}
	pool.close()
}

func TestFirefoxPoolDoesNotCountBrowserSlotAcquisitionCancellation(t *testing.T) {
	observer := &recordedBrowserPoolObservation{}
	pool, release, first := newSaturatedFirefoxPool(t, observer)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pool.render(ctx, "https://example.org/second")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("second render error = %v, want context cancellation", err)
	}
	if _, _, failures := observer.snapshot(); len(failures) != 0 {
		t.Fatalf("browser slot acquisition failures = %v, want none", failures)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first render: %v", err)
	}
	pool.close()
}

func TestFirefoxPoolDoesNotCountShutdownWhileWaitingForBrowserSlot(t *testing.T) {
	observer := &recordedBrowserPoolObservation{}
	pool, release, first := newSaturatedFirefoxPool(t, observer)
	second := make(chan error, 1)
	go func() {
		_, err := pool.render(context.Background(), "https://example.org/second")
		second <- err
	}()
	waitUntil := time.Now().Add(time.Second)
	for len(pool.selection) != 0 && time.Now().Before(waitUntil) {
		time.Sleep(time.Millisecond)
	}
	if len(pool.selection) != 0 {
		t.Fatal("second render did not reach the browser slot wait")
	}
	closed := make(chan struct{})
	go func() {
		pool.close()
		close(closed)
	}()
	select {
	case err := <-second:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second render error = %v, want shutdown cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pool shutdown did not cancel the browser slot wait")
	}
	if _, _, failures := observer.snapshot(); len(failures) != 0 {
		t.Fatalf("browser slot acquisition failures = %v, want none", failures)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first render: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("pool shutdown did not finish")
	}
}

func TestFirefoxPoolNilBrowserSlotAcquisitionObserverStaysNoop(t *testing.T) {
	pool := newFirefoxPoolObserved(
		BrowserLaunch{Sessions: 1},
		"http://proxy.example",
		func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return nil, errors.New("unexpected launch")
		},
		browserPoolObservation{},
	)
	<-pool.selection
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err := pool.render(ctx, "https://example.org/")
	pool.selection <- struct{}{}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("render error = %v, want context deadline", err)
	}
	pool.close()
}
