package firefoxfetch

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFirefoxPoolRendersOnTwoSessionsConcurrently(t *testing.T) {
	started := make(chan struct{}, maximumFirefoxSessions)
	release := make(chan struct{})
	var launches atomic.Int32
	pool := newFirefoxPool(
		BrowserLaunch{Sessions: maximumFirefoxSessions + 1},
		"http://proxy.example",
		func(context.Context, BrowserLaunch, string) (browserSession, error) {
			launches.Add(1)
			return &fakeSession{
				aliveVal: true,
				renderFunc: func(
					context.Context,
					string,
					time.Duration,
				) (renderedPage, error) {
					started <- struct{}{}
					<-release

					return renderedPage{url: "https://example.org/"}, nil
				},
			}, nil
		},
	)
	done := make(chan error, maximumFirefoxSessions)
	for range maximumFirefoxSessions {
		go func() {
			_, err := pool.render(t.Context(), "https://example.org/")
			done <- err
		}()
	}
	for range maximumFirefoxSessions {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("browser pool serialized concurrent renders")
		}
	}
	close(release)
	for range maximumFirefoxSessions {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if launches.Load() != maximumFirefoxSessions {
		t.Fatalf("launches = %d, want %d", launches.Load(), maximumFirefoxSessions)
	}
	pool.close()
}

func TestFirefoxPoolRelaunchesSessionsAfterRedirectLimitChange(t *testing.T) {
	var launches []int
	var sessions []*fakeSession
	pool := newFirefoxPool(
		BrowserLaunch{Sessions: 1, MaxRedirects: 10},
		"http://proxy.example",
		func(_ context.Context, launch BrowserLaunch, _ string) (browserSession, error) {
			launches = append(launches, launch.MaxRedirects)
			session := &fakeSession{
				aliveVal:   true,
				renderFunc: staticRender("https://example.org/"),
			}
			sessions = append(sessions, session)

			return session, nil
		},
	)
	defer pool.close()
	fetcher := &BrowserPageFetcher{pool: pool}
	if _, err := pool.render(t.Context(), "https://example.org/first"); err != nil {
		t.Fatalf("initial render: %v", err)
	}
	fetcher.SetMaxRedirects(10)
	if _, err := pool.render(t.Context(), "https://example.org/same"); err != nil {
		t.Fatalf("render after unchanged redirect limit: %v", err)
	}
	if len(launches) != 1 || sessions[0].closed != 0 {
		t.Fatalf("unchanged redirect limit restarted browser: %v/%d", launches, sessions[0].closed)
	}
	pool.setMaxRedirects(-1)
	pool.managers[0].setMaxRedirects(-1)
	fetcher.SetMaxRedirects(2)
	if len(sessions) != 1 || sessions[0].closed != 0 {
		t.Fatalf("old browser sessions = %+v", sessions)
	}
	if _, err := pool.render(t.Context(), "https://example.org/second"); err != nil {
		t.Fatalf("render after redirect update: %v", err)
	}
	if sessions[0].closed != 1 {
		t.Fatalf("old browser session closed %d times, want once", sessions[0].closed)
	}
	if len(launches) != 2 || launches[0] != 10 || launches[1] != 2 {
		t.Fatalf("browser redirect limits = %v, want [10 2]", launches)
	}
}

func TestFirefoxPoolRelaunchesSessionsAfterSandboxChange(t *testing.T) {
	var launches []bool
	var sessions []*fakeSession
	pool := newFirefoxPool(
		BrowserLaunch{Sessions: 1, Sandbox: true},
		"http://proxy.example",
		func(_ context.Context, launch BrowserLaunch, _ string) (browserSession, error) {
			launches = append(launches, launch.Sandbox)
			session := &fakeSession{
				aliveVal:   true,
				renderFunc: staticRender("https://example.org/"),
			}
			sessions = append(sessions, session)

			return session, nil
		},
	)
	defer pool.close()
	fetcher := &BrowserPageFetcher{pool: pool}
	if _, err := pool.render(t.Context(), "https://example.org/first"); err != nil {
		t.Fatalf("initial render: %v", err)
	}
	fetcher.SetSandbox(true)
	if _, err := pool.render(t.Context(), "https://example.org/same"); err != nil {
		t.Fatalf("render after unchanged sandbox: %v", err)
	}
	if len(launches) != 1 || sessions[0].closed != 0 {
		t.Fatalf("unchanged sandbox restarted browser: %v/%d", launches, sessions[0].closed)
	}
	fetcher.SetSandbox(false)
	if len(sessions) != 1 || sessions[0].closed != 0 {
		t.Fatalf("browser closed before its next render: %+v", sessions)
	}
	if _, err := pool.render(t.Context(), "https://example.org/second"); err != nil {
		t.Fatalf("render after sandbox update: %v", err)
	}
	if sessions[0].closed != 1 {
		t.Fatalf("old browser session closed %d times, want once", sessions[0].closed)
	}
	if len(launches) != 2 || !launches[0] || launches[1] {
		t.Fatalf("browser sandbox launches = %v, want [true false]", launches)
	}
}

func TestFirefoxPoolSandboxChangeDoesNotWaitForActiveRender(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	pool := newFirefoxPool(
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

					return renderedPage{url: "https://example.org/"}, nil
				},
			}, nil
		},
	)
	defer pool.close()
	rendered := make(chan error, 1)
	go func() {
		_, err := pool.render(t.Context(), "https://example.org/")
		rendered <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("browser render did not start")
	}
	updated := make(chan struct{})
	go func() {
		pool.setSandbox(true)
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sandbox update waited for the active browser render")
	}
	close(release)
	if err := <-rendered; err != nil {
		t.Fatalf("active render: %v", err)
	}
}

func TestFirefoxPoolRedirectLimitChangeDoesNotWaitForActiveRender(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	pool := newFirefoxPool(
		BrowserLaunch{Sessions: 1, MaxRedirects: 10},
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

					return renderedPage{url: "https://example.org/"}, nil
				},
			}, nil
		},
	)
	defer pool.close()
	rendered := make(chan error, 1)
	go func() {
		_, err := pool.render(t.Context(), "https://example.org/")
		rendered <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("browser render did not start")
	}
	updated := make(chan struct{})
	go func() {
		pool.setMaxRedirects(2)
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("redirect update waited for the active browser render")
	}
	close(release)
	if err := <-rendered; err != nil {
		t.Fatalf("active render: %v", err)
	}
}

func TestFirefoxPoolHonorsContextWhileAllSessionsAreBusy(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	pool := newFirefoxPool(
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
	)
	done := make(chan struct{})
	go func() {
		_, _ = pool.render(context.Background(), "https://example.org/first")
		close(done)
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() {
		_, err := pool.render(ctx, "https://example.org/second")
		second <- err
	}()
	deadline := time.Now().Add(time.Second)
	for len(pool.selection) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(pool.selection) != 0 {
		t.Fatal("second render did not reach session wait")
	}
	cancel()
	if err := <-second; !errors.Is(err, context.Canceled) {
		t.Fatalf("render error = %v, want context cancellation", err)
	}
	close(release)
	<-done
	pool.close()
}

func TestFirefoxPoolUsesHealthySessionWhileAnotherCoolsDown(t *testing.T) {
	pool := newFirefoxPool(
		BrowserLaunch{
			Sessions:         2,
			FailureThreshold: 1,
			FailureCooldown:  time.Hour,
		},
		"http://proxy.example",
		func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return nil, errors.New("unused starter")
		},
	)
	var brokenStarts atomic.Int32
	pool.managers[0].start = func(context.Context, BrowserLaunch, string) (browserSession, error) {
		brokenStarts.Add(1)

		return nil, errors.New("broken slot")
	}
	pool.managers[1].start = func(context.Context, BrowserLaunch, string) (browserSession, error) {
		return &fakeSession{
			aliveVal:   true,
			renderFunc: staticRender("https://example.org/"),
		}, nil
	}
	if _, err := pool.render(t.Context(), "https://example.org/first"); err == nil {
		t.Fatal("broken session should fail its first render")
	}
	for range 3 {
		if _, err := pool.render(t.Context(), "https://example.org/healthy"); err != nil {
			t.Fatalf("healthy session render: %v", err)
		}
	}
	if brokenStarts.Load() != 1 {
		t.Fatalf("broken session launches = %d, want 1 during cooldown", brokenStarts.Load())
	}
	pool.close()
}

func TestFirefoxPoolReportsWhenEverySessionCoolsDown(t *testing.T) {
	pool := newFirefoxPool(
		BrowserLaunch{Sessions: 2},
		"http://proxy.example",
		func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return nil, errors.New("unexpected launch")
		},
	)
	retryAfter := time.Now().Add(time.Hour)
	for _, manager := range pool.managers {
		manager.retryAfter = retryAfter
	}
	if _, err := pool.render(t.Context(), "https://example.org/"); err == nil ||
		!strings.Contains(err.Error(), "all firefox sessions cooling down") {
		t.Fatalf("render error = %v", err)
	}
	pool.close()
}

func TestFirefoxPoolConcurrentCooldownChecksComplete(t *testing.T) {
	pool := newFirefoxPool(
		BrowserLaunch{Sessions: 2},
		"http://proxy.example",
		func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return nil, errors.New("unexpected launch")
		},
	)
	retryAfter := time.Now().Add(time.Hour)
	for _, manager := range pool.managers {
		manager.retryAfter = retryAfter
		manager.mu.Lock()
	}
	locked := true
	defer func() {
		if locked {
			for _, manager := range pool.managers {
				manager.mu.Unlock()
			}
		}
		pool.close()
	}()
	done := make(chan error, 2)
	go func() {
		_, err := pool.render(t.Context(), "https://example.org/first")
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for len(pool.available) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(pool.available) != 1 {
		t.Fatal("first cooldown check did not acquire a session")
	}
	go func() {
		_, err := pool.render(t.Context(), "https://example.org/second")
		done <- err
	}()
	for _, manager := range pool.managers {
		manager.mu.Unlock()
	}
	locked = false
	for range 2 {
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "all firefox sessions cooling down") {
				t.Fatalf("render error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent cooldown checks blocked each other")
		}
	}
}

func TestFirefoxPoolSelectionWaitHonorsCancellation(t *testing.T) {
	pool := newFirefoxPool(
		BrowserLaunch{Sessions: 1},
		"http://proxy.example",
		func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return nil, errors.New("unexpected launch")
		},
	)
	<-pool.selection
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := pool.acquireRenderable(ctx)
	pool.selection <- struct{}{}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("selection error = %v, want context cancellation", err)
	}
	pool.close()
}

func TestFirefoxPoolWaitsForBusyHealthySessionInsteadOfCoolingSlot(t *testing.T) {
	pool := newFirefoxPool(
		BrowserLaunch{
			Sessions:         2,
			FailureThreshold: 1,
			FailureCooldown:  time.Hour,
		},
		"http://proxy.example",
		func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return nil, errors.New("unused starter")
		},
	)
	pool.managers[0].start = func(context.Context, BrowserLaunch, string) (browserSession, error) {
		return nil, errors.New("broken slot")
	}
	busy := make(chan struct{})
	release := make(chan struct{})
	pool.managers[1].start = func(context.Context, BrowserLaunch, string) (browserSession, error) {
		return &fakeSession{
			aliveVal: true,
			renderFunc: func(
				_ context.Context,
				rawURL string,
				_ time.Duration,
			) (renderedPage, error) {
				if rawURL == "https://example.org/busy" {
					close(busy)
					<-release
				}

				return renderedPage{url: rawURL}, nil
			},
		}, nil
	}
	if _, err := pool.render(t.Context(), "https://example.org/quarantine"); err == nil {
		t.Fatal("broken session should enter cooldown")
	}
	first := make(chan error, 1)
	go func() {
		_, err := pool.render(t.Context(), "https://example.org/busy")
		first <- err
	}()
	<-busy
	second := make(chan error, 1)
	go func() {
		_, err := pool.render(t.Context(), "https://example.org/waiter")
		second <- err
	}()
	select {
	case err := <-second:
		t.Fatalf("waiter returned before healthy session was available: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("busy render: %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("waiting render: %v", err)
	}
	pool.close()
}

func TestFirefoxPoolCloseCancelsLaunchAndPreventsRelaunch(t *testing.T) {
	started := make(chan struct{})
	var launches atomic.Int32
	pool := newFirefoxPool(
		BrowserLaunch{Sessions: 1},
		"http://proxy.example",
		func(ctx context.Context, _ BrowserLaunch, _ string) (browserSession, error) {
			launches.Add(1)
			close(started)
			<-ctx.Done()

			return nil, ctx.Err()
		},
	)
	rendered := make(chan error, 1)
	go func() {
		_, err := pool.render(context.Background(), "https://example.org/")
		rendered <- err
	}()
	<-started
	closed := make(chan struct{})
	go func() {
		pool.close()
		close(closed)
	}()
	select {
	case err := <-rendered:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("render error = %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pool close did not cancel browser launch")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("pool close did not finish")
	}
	if _, err := pool.render(
		context.Background(),
		"https://example.org/again",
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("post-close render error = %v, want cancellation", err)
	}
	if launches.Load() != 1 {
		t.Fatalf("launches after close = %d, want 1", launches.Load())
	}
}
