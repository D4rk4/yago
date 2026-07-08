package visitintake_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacyvisitcrawl/internal/visitintake"
)

type recordingPlacement struct {
	order    yacycrawlcontract.CrawlOrder
	attempts int
}

func (p *recordingPlacement) Attempt(order yacycrawlcontract.CrawlOrder) {
	p.order = order
	p.attempts++
}

type recordingMetrics struct {
	mu       sync.Mutex
	received int
	rejected int
	placed   int
	unplaced int
}

func (m *recordingMetrics) VisitReceived() { m.mu.Lock(); defer m.mu.Unlock(); m.received++ }
func (m *recordingMetrics) VisitRejected() { m.mu.Lock(); defer m.mu.Unlock(); m.rejected++ }
func (m *recordingMetrics) OrderPlaced()   { m.mu.Lock(); defer m.mu.Unlock(); m.placed++ }
func (m *recordingMetrics) OrderUnplaced() { m.mu.Lock(); defer m.mu.Unlock(); m.unplaced++ }

func (m *recordingMetrics) placedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.placed
}

func (m *recordingMetrics) unplacedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unplaced
}

func mount(
	placement visitintake.CrawlOrderPlacement,
	metrics visitintake.VisitMetrics,
) *http.ServeMux {
	mux := http.NewServeMux()
	profile := yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Scope: yacycrawlcontract.ScopeDomain,
	})
	visitintake.MountVisitIntake(mux, placement, profile, metrics, 1<<10)
	return mux
}

func get(mux *http.ServeMux, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestVisitRedirectsAndAttemptsPlacement(t *testing.T) {
	placement := &recordingPlacement{}
	metrics := &recordingMetrics{}
	rec := get(
		mount(placement, metrics),
		visitintake.PathVisit+"?url=https%3A%2F%2Fexample.org%2Fa",
	)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "https://example.org/a" {
		t.Fatalf("location = %q", got)
	}
	if placement.attempts != 1 {
		t.Fatalf("attempts = %d, want 1", placement.attempts)
	}
	if len(placement.order.SeedURLs) != 1 ||
		placement.order.SeedURLs[0] != "https://example.org/a" {
		t.Fatalf("order seeds = %v", placement.order.SeedURLs)
	}
	if placement.order.OrderID == "" {
		t.Fatal("order id is empty")
	}
	if metrics.received != 1 {
		t.Fatalf("received = %d, want 1", metrics.received)
	}
}

func TestVisitRejectsMissingURL(t *testing.T) {
	placement := &recordingPlacement{}
	metrics := &recordingMetrics{}
	rec := get(mount(placement, metrics), visitintake.PathVisit)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if placement.attempts != 0 {
		t.Fatal("placement should not be attempted")
	}
	if metrics.rejected != 1 {
		t.Fatalf("rejected = %d, want 1", metrics.rejected)
	}
}

func TestVisitRejectsNonHTTPScheme(t *testing.T) {
	rec := get(mount(&recordingPlacement{}, &recordingMetrics{}),
		visitintake.PathVisit+"?url=ftp%3A%2F%2Fexample.org%2Fa")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestVisitRejectsMissingHost(t *testing.T) {
	rec := get(mount(&recordingPlacement{}, &recordingMetrics{}),
		visitintake.PathVisit+"?url=https%3A%2F%2F")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestVisitRejectsNonGet(t *testing.T) {
	mux := mount(&recordingPlacement{}, &recordingMetrics{})
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, visitintake.PathVisit, nil,
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
