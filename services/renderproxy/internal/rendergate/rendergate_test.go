package rendergate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/renderedpage"
)

type stubRenderer struct {
	page  renderedpage.Page
	err   error
	delay time.Duration

	mu          sync.Mutex
	inFlight    int
	maxInFlight int
}

func (s *stubRenderer) Render(ctx context.Context, targetURL string) (renderedpage.Page, error) {
	s.mu.Lock()
	s.inFlight++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
	s.mu.Unlock()

	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
		return renderedpage.Page{}, fmt.Errorf("stub render canceled: %w", ctx.Err())
	}

	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
	return s.page, s.err
}

type stubMetrics struct {
	waited    atomic.Int64
	succeeded atomic.Int64
	failed    atomic.Int64
}

func (m *stubMetrics) RenderWaited()                { m.waited.Add(1) }
func (m *stubMetrics) RenderSucceeded()             { m.succeeded.Add(1) }
func (m *stubMetrics) RenderFailed(string)          { m.failed.Add(1) }
func (m *stubMetrics) RenderObserved(time.Duration) {}

func TestRenderCapsConcurrency(t *testing.T) {
	inner := &stubRenderer{delay: 20 * time.Millisecond}
	metrics := &stubMetrics{}
	gated := New(inner, 2, time.Second, 1024, metrics)

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			_, _ = gated.Render(context.Background(), "http://example.com")
		})
	}
	wg.Wait()

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if inner.maxInFlight > 2 {
		t.Fatalf("max in-flight = %d, want <= 2", inner.maxInFlight)
	}
	if metrics.waited.Load() == 0 {
		t.Fatal("expected at least one wait to be recorded")
	}
}

func TestRenderFailsWhenPageTooLarge(t *testing.T) {
	inner := &stubRenderer{page: renderedpage.Page{Body: make([]byte, 100)}}
	metrics := &stubMetrics{}
	gated := New(inner, 1, time.Second, 10, metrics)

	if _, err := gated.Render(context.Background(), "http://example.com"); err == nil {
		t.Fatal("expected error for oversized page")
	}
	if metrics.failed.Load() != 1 {
		t.Fatalf("failed count = %d, want 1", metrics.failed.Load())
	}
}

func TestRenderAppliesDeadline(t *testing.T) {
	inner := &stubRenderer{delay: 50 * time.Millisecond}
	metrics := &stubMetrics{}
	gated := New(inner, 1, 5*time.Millisecond, 1024, metrics)

	_, err := gated.Render(context.Background(), "http://example.com")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestRenderPropagatesInnerError(t *testing.T) {
	inner := &stubRenderer{err: errors.New("boom")}
	metrics := &stubMetrics{}
	gated := New(inner, 1, time.Second, 1024, metrics)

	if _, err := gated.Render(context.Background(), "http://example.com"); err == nil {
		t.Fatal("expected error")
	}
	if metrics.succeeded.Load() != 0 {
		t.Fatal("did not expect success recorded")
	}
}
