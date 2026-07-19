package firefoxfetch

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagoegress"
)

type recordedBrowserPoolObservation struct {
	mu       sync.Mutex
	waits    []time.Duration
	states   []BrowserPoolState
	failures []BrowserFailureReason
}

type orderedBrowserPoolObservation struct {
	mu        sync.Mutex
	states    []BrowserPoolState
	armed     bool
	blocked   chan struct{}
	release   chan struct{}
	blockOnce sync.Once
}

func (o *orderedBrowserPoolObservation) ObserveBrowserSlotWait(time.Duration) {}

func (o *orderedBrowserPoolObservation) ObserveBrowserFailure(BrowserFailureReason) {}

func (o *orderedBrowserPoolObservation) ObserveBrowserPoolState(state BrowserPoolState) {
	o.mu.Lock()
	o.states = append(o.states, state)
	armed := o.armed
	o.mu.Unlock()
	if armed && state.Busy == 1 {
		o.blockOnce.Do(func() { close(o.blocked) })
		<-o.release
	}
}

func (o *orderedBrowserPoolObservation) arm() {
	o.mu.Lock()
	o.armed = true
	o.mu.Unlock()
}

func (o *orderedBrowserPoolObservation) lastState() BrowserPoolState {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.states[len(o.states)-1]
}

func (o *recordedBrowserPoolObservation) ObserveBrowserSlotWait(elapsed time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.waits = append(o.waits, elapsed)
}

func (o *recordedBrowserPoolObservation) ObserveBrowserPoolState(state BrowserPoolState) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.states = append(o.states, state)
}

func (o *recordedBrowserPoolObservation) ObserveBrowserFailure(reason BrowserFailureReason) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.failures = append(o.failures, reason)
}

func (o *recordedBrowserPoolObservation) snapshot() (
	[]time.Duration,
	[]BrowserPoolState,
	[]BrowserFailureReason,
) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]time.Duration(nil), o.waits...),
		append([]BrowserPoolState(nil), o.states...),
		append([]BrowserFailureReason(nil), o.failures...)
}

func TestBrowserFailureErrorPreservesCauseAndReason(t *testing.T) {
	want := errors.New("browser unavailable")
	failure := browserFailureError{reason: BrowserFailureLaunch, cause: want}
	if !errors.Is(failure, want) ||
		!strings.Contains(failure.Error(), string(BrowserFailureLaunch)) {
		t.Fatalf("browser failure = %v", failure)
	}
	if reason, ok := browserFailureReason(failure); !ok || reason != BrowserFailureLaunch {
		t.Fatalf("browser failure reason = %q, %t", reason, ok)
	}
}

func TestNewBrowserPageFetcherConnectsPoolObserver(t *testing.T) {
	observer := &recordedBrowserPoolObservation{}
	fetcher, closeFetcher, err := NewBrowserPageFetcherWithPoolObservation(
		BrowserLaunch{
			Timeout:            time.Second,
			executableResolver: acceptTestFirefoxExecutable,
		},
		yagoegress.NewGuard(false),
		observer,
	)
	if err != nil {
		t.Fatalf("new observed browser fetcher: %v", err)
	}
	defer closeFetcher()
	if fetcher == nil || fetcher.pool == nil {
		t.Fatal("observed browser fetcher was not assembled")
	}
	_, states, _ := observer.snapshot()
	if !containsBrowserPoolState(states, BrowserPoolState{Ready: maximumFirefoxSessions}) {
		t.Fatalf("initial observed browser pool states = %+v", states)
	}
}

func TestFirefoxPoolObservesWaitAndSessionState(t *testing.T) {
	observer := &recordedBrowserPoolObservation{}
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
					return renderedPage{}, nil
				},
			}, nil
		},
		browserPoolObservation{observer: observer},
	)
	done := make(chan error, 1)
	go func() {
		_, err := pool.render(t.Context(), "https://example.org/")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("render did not start")
	}
	_, states, _ := observer.snapshot()
	if !containsBrowserPoolState(states, BrowserPoolState{Busy: 1}) {
		t.Fatalf("states = %+v, want busy session", states)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("render: %v", err)
	}
	waits, states, failures := observer.snapshot()
	if len(waits) != 1 || waits[0] < 0 {
		t.Fatalf("waits = %v, want one non-negative duration", waits)
	}
	if !containsBrowserPoolState(states, BrowserPoolState{Ready: 1}) {
		t.Fatalf("states = %+v, want ready session", states)
	}
	for _, state := range states {
		if state.Ready+state.Busy+state.Cooling != 1 {
			t.Fatalf("state = %+v, want one classified session", state)
		}
	}
	if len(failures) != 0 {
		t.Fatalf("failures = %v, want none", failures)
	}
	pool.close()
}

func TestFirefoxPoolObservesBoundedOperationalFailures(t *testing.T) {
	observer := &recordedBrowserPoolObservation{}
	pool := newFirefoxPoolObserved(
		BrowserLaunch{
			Sessions:         1,
			FailureThreshold: 1,
			FailureCooldown:  time.Hour,
		},
		"http://proxy.example",
		func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return nil, errors.New("launch unavailable")
		},
		browserPoolObservation{observer: observer},
	)
	if _, err := pool.render(t.Context(), "https://example.org/first"); err == nil {
		t.Fatal("launch failure is nil")
	}
	if _, err := pool.render(t.Context(), "https://example.org/second"); err == nil {
		t.Fatal("cooldown failure is nil")
	}
	_, states, failures := observer.snapshot()
	if !containsBrowserPoolState(states, BrowserPoolState{Cooling: 1}) {
		t.Fatalf("states = %+v, want cooling session", states)
	}
	if !containsBrowserFailure(failures, BrowserFailureLaunch) ||
		!containsBrowserFailure(failures, BrowserFailureCooldown) {
		t.Fatalf("failures = %v, want launch and cooldown", failures)
	}
	pool.close()
}

func TestFirefoxPoolObservesRenderAndSlotDeadlineFailures(t *testing.T) {
	observer := &recordedBrowserPoolObservation{}
	started := make(chan struct{})
	release := make(chan struct{})
	pool := newFirefoxPoolObserved(
		BrowserLaunch{Sessions: 1},
		"http://proxy.example",
		func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return &fakeSession{
				aliveVal: true,
				renderFunc: func(
					_ context.Context,
					rawURL string,
					_ time.Duration,
				) (renderedPage, error) {
					if rawURL == "https://example.org/busy" {
						close(started)
						<-release
						return renderedPage{}, nil
					}
					return renderedPage{}, errors.New("render unavailable")
				},
			}, nil
		},
		browserPoolObservation{observer: observer},
	)
	first := make(chan error, 1)
	go func() {
		_, err := pool.render(t.Context(), "https://example.org/busy")
		first <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("busy render did not start")
	}
	deadlineContext, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := pool.render(
		deadlineContext,
		"https://example.org/waiting",
	); !errors.Is(
		err,
		context.DeadlineExceeded,
	) {
		t.Fatalf("slot wait error = %v, want deadline", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("busy render: %v", err)
	}
	if _, err := pool.render(t.Context(), "https://example.org/render-error"); err == nil {
		t.Fatal("render failure is nil")
	}
	_, _, failures := observer.snapshot()
	if !containsBrowserFailure(failures, BrowserFailureSlotDeadline) ||
		!containsBrowserFailure(failures, BrowserFailureRender) {
		t.Fatalf("failures = %v, want slot deadline and render", failures)
	}
	pool.close()
}

func TestFirefoxPoolPublishesNewestConcurrentSessionStateLast(t *testing.T) {
	observer := &orderedBrowserPoolObservation{
		blocked: make(chan struct{}),
		release: make(chan struct{}),
	}
	started := make(chan string, 2)
	releases := map[string]chan struct{}{
		"https://example.org/first":  make(chan struct{}),
		"https://example.org/second": make(chan struct{}),
	}
	pool := newFirefoxPoolObserved(
		BrowserLaunch{Sessions: 2},
		"http://proxy.example",
		func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return &fakeSession{
				aliveVal: true,
				renderFunc: func(
					_ context.Context,
					rawURL string,
					_ time.Duration,
				) (renderedPage, error) {
					started <- rawURL
					<-releases[rawURL]
					return renderedPage{}, nil
				},
			}, nil
		},
		browserPoolObservation{observer: observer},
	)
	done := make(chan error, 2)
	for _, rawURL := range []string{
		"https://example.org/first",
		"https://example.org/second",
	} {
		go func() {
			_, err := pool.render(t.Context(), rawURL)
			done <- err
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("concurrent render did not start")
		}
	}
	observer.arm()
	close(releases["https://example.org/first"])
	select {
	case <-observer.blocked:
	case <-time.After(time.Second):
		t.Fatal("older busy observation did not block")
	}
	close(releases["https://example.org/second"])
	close(observer.release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("concurrent render: %v", err)
		}
	}
	if got := observer.lastState(); got != (BrowserPoolState{Ready: 2}) {
		t.Fatalf("last state = %+v, want both sessions ready", got)
	}
	pool.close()
}

func TestFirefoxPoolDoesNotReportExpiredCooldownAsAnotherSession(t *testing.T) {
	observer := &recordedBrowserPoolObservation{}
	started := make(chan struct{})
	release := make(chan struct{})
	pool := newFirefoxPoolObserved(
		BrowserLaunch{Sessions: 2},
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
					return renderedPage{}, nil
				},
			}, nil
		},
		browserPoolObservation{observer: observer},
	)
	pool.managers[0].mu.Lock()
	pool.managers[0].retryAfter = time.Now().Add(-time.Second)
	pool.managers[0].cooling.Store(true)
	pool.managers[0].mu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := pool.render(t.Context(), "https://example.org/")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expired-cooldown render did not start")
	}
	_, states, _ := observer.snapshot()
	if !containsBrowserPoolState(states, BrowserPoolState{Ready: 1, Busy: 1}) {
		t.Fatalf("states = %+v, want one busy and one ready session", states)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("expired-cooldown render: %v", err)
	}
	pool.close()
}

func containsBrowserPoolState(states []BrowserPoolState, wanted BrowserPoolState) bool {
	for _, state := range states {
		if state == wanted {
			return true
		}
	}
	return false
}

func containsBrowserFailure(
	failures []BrowserFailureReason,
	wanted BrowserFailureReason,
) bool {
	for _, failure := range failures {
		if failure == wanted {
			return true
		}
	}
	return false
}
