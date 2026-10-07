package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type crawlOrderClearMonitorFake struct {
	snapshot CrawlMonitor
}

func (source *crawlOrderClearMonitorFake) Monitor(context.Context) CrawlMonitor {
	return source.snapshot
}

type crawlOrderClearSourceFake struct {
	monitor              *crawlOrderClearMonitorFake
	removed              int
	err                  error
	calls                int
	pendingAfterClear    int
	setPendingAfterClear bool
}

func (source *crawlOrderClearSourceFake) ClearPendingOrders(context.Context) (int, error) {
	source.calls++
	if source.monitor != nil {
		if source.setPendingAfterClear {
			source.monitor.snapshot.QueuePending = source.pendingAfterClear
		} else {
			source.monitor.snapshot.QueuePending -= source.removed
		}
	}

	return source.removed, source.err
}

func crawlOrderClearOptions(
	t *testing.T,
	snapshot CrawlMonitor,
	source *crawlOrderClearSourceFake,
) Options {
	t.Helper()
	monitor := &crawlOrderClearMonitorFake{snapshot: snapshot}
	source.monitor = monitor
	return Options{
		Crawl:                 &fakeCrawl{},
		Monitor:               monitor,
		CrawlOrderClearSource: source,
	}
}

func TestCrawlOrderClearConfirmationShowsQueueCountsAndNativeForm(t *testing.T) {
	options := crawlOrderClearOptions(t, CrawlMonitor{
		QueueAvailable: true,
		QueuePending:   5,
		QueueLeased:    2,
	}, &crawlOrderClearSourceFake{})

	got := do(t, New(options), "/admin/crawl/queue/clear")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", got.status)
	}
	for _, want := range []string{
		"Clear pending orders", "5 pending orders", "2 leased orders",
		`method="post" action="/admin/crawl/queue/clear"`,
		`name="confirmation"`, "CLEAR PENDING ORDERS", "required",
		`name="csrf_token"`, `href="/admin/crawl"`,
		"The node fixes a boundary before deleting pending orders",
		"Orders accepted after that boundary remain queued",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("confirmation page missing %q", want)
		}
	}
	if strings.Contains(got.body, `hx-trigger="every 5s"`) {
		t.Fatal("confirmation page polls and can reset the confirmation field")
	}
}

func TestCrawlMonitorLinksToOrderClearOnlyWhenQueueCanBeCleared(t *testing.T) {
	tests := []struct {
		name     string
		snapshot CrawlMonitor
		clearer  bool
		wantLink bool
	}{
		{
			name:     "known pending orders",
			snapshot: CrawlMonitor{QueueAvailable: true, QueuePending: 2},
			clearer:  true,
			wantLink: true,
		},
		{
			name:     "unavailable queue",
			snapshot: CrawlMonitor{QueuePending: 2},
			clearer:  true,
		},
		{
			name:     "empty queue",
			snapshot: CrawlMonitor{QueueAvailable: true},
			clearer:  true,
		},
		{
			name:     "missing clear source",
			snapshot: CrawlMonitor{QueueAvailable: true, QueuePending: 2},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := Options{
				Crawl:   &fakeCrawl{},
				Monitor: fakeMonitor{snap: test.snapshot},
				Control: &fakeControl{},
			}
			if test.clearer {
				options = crawlOrderClearOptions(t, test.snapshot, &crawlOrderClearSourceFake{})
				options.Control = &fakeControl{}
			}
			got := do(t, New(options), "/admin/crawl/monitor")
			link := strings.Contains(got.body, `href="/admin/crawl/queue/clear"`)
			if link != test.wantLink {
				t.Fatalf("clear link = %v, want %v", link, test.wantLink)
			}
		})
	}
}

func TestCrawlMonitorCopyDistinguishesOrdersFromPendingURLs(t *testing.T) {
	options := crawlOrderClearOptions(t, sampleMonitor(), &crawlOrderClearSourceFake{})
	options.Control = &fakeControl{}
	got := do(t, New(options), "/admin/crawl/monitor")
	for _, want := range []string{
		"Node order queue", "Pending URLs", "Run Cancel drops only that run’s pending URLs",
		"Clear pending orders leaves leased orders and active fetches alone",
		"Orders accepted after the deletion boundary is set remain queued",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("monitor copy missing %q", want)
		}
	}
}

func TestCrawlOrderClearConfirmationOmitsUnavailableOrEmptyAction(t *testing.T) {
	tests := []struct {
		name     string
		snapshot CrawlMonitor
		message  string
	}{
		{
			name:     "unavailable queue",
			snapshot: CrawlMonitor{QueuePending: 3, QueueLeased: 1},
			message:  "Pending order counts are unavailable",
		},
		{
			name: "empty queue",
			snapshot: CrawlMonitor{
				QueueAvailable: true,
				QueueLeased:    1,
			},
			message: "No pending orders are waiting",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := crawlOrderClearOptions(t, test.snapshot, &crawlOrderClearSourceFake{})
			got := do(t, New(options), "/admin/crawl/queue/clear")
			if got.status != http.StatusOK || !strings.Contains(got.body, test.message) {
				t.Fatalf("response = %d %q", got.status, got.body)
			}
			if strings.Contains(got.body, `method="post" action="/admin/crawl/queue/clear"`) {
				t.Fatal("clear form rendered without a known nonempty queue")
			}
		})
	}
}

func TestCrawlOrderClearConfirmationIsUnavailableWithoutSource(t *testing.T) {
	console := New(Options{Monitor: &crawlOrderClearMonitorFake{
		snapshot: CrawlMonitor{QueueAvailable: true, QueuePending: 2},
	}})
	got := do(t, console, "/admin/crawl/queue/clear")
	if got.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 without a clear source", got.status)
	}
	got = doPost(t, console, "/admin/crawl/queue/clear", url.Values{
		"confirmation": {"CLEAR PENDING ORDERS"},
	})
	if got.status != http.StatusNotFound {
		t.Fatalf("POST status = %d, want 404 without a clear source", got.status)
	}
}

func TestCrawlOrderClearRequiresMonitorForConfirmationAndAction(t *testing.T) {
	source := &crawlOrderClearSourceFake{}
	console := New(Options{
		Crawl:                 &fakeCrawl{},
		CrawlOrderClearSource: source,
	})

	got := do(t, console, "/admin/crawl/queue/clear")
	if got.status != http.StatusOK ||
		!strings.Contains(got.body, "Pending order counts are unavailable. Nothing was changed.") {
		t.Fatalf("confirmation without monitor = %d %q", got.status, got.body)
	}
	if strings.Contains(got.body, `method="post" action="/admin/crawl/queue/clear"`) {
		t.Fatal("clear form rendered without an order monitor")
	}

	got = doPost(t, console, "/admin/crawl/queue/clear", url.Values{
		"confirmation": {crawlOrderClearConfirmation},
	})
	const unavailableResponse = "Pending order counts are unavailable; no orders were cleared."
	if got.status != http.StatusOK || !strings.Contains(got.body, unavailableResponse) {
		t.Fatalf("action without monitor = %d %q", got.status, got.body)
	}
	if source.calls != 0 {
		t.Fatalf("clear calls = %d, want 0 without an order monitor", source.calls)
	}
}

func TestCrawlOrderClearRejectsMissingOrWrongConfirmation(t *testing.T) {
	for _, confirmation := range []string{"", "clear pending orders", "CLEAR PENDING ORDER"} {
		t.Run(confirmation, func(t *testing.T) {
			source := &crawlOrderClearSourceFake{}
			options := crawlOrderClearOptions(t, CrawlMonitor{
				QueueAvailable: true,
				QueuePending:   2,
			}, source)
			got := doPost(t, New(options), "/admin/crawl/queue/clear", url.Values{
				"confirmation": {confirmation},
			})
			if got.status != http.StatusOK ||
				!strings.Contains(got.body, "Confirmation did not match") {
				t.Fatalf("response = %d %q", got.status, got.body)
			}
			if source.calls != 0 {
				t.Fatalf("clear calls = %d, want 0", source.calls)
			}
		})
	}
}

func TestCrawlOrderClearReportsRemovedCountAndFreshQueueDepth(t *testing.T) {
	source := &crawlOrderClearSourceFake{removed: 3}
	options := crawlOrderClearOptions(t, CrawlMonitor{
		QueueAvailable: true,
		QueuePending:   7,
		QueueLeased:    2,
	}, source)
	got := doPost(t, New(options), "/admin/crawl/queue/clear", url.Values{
		"confirmation": {"CLEAR PENDING ORDERS"},
	})
	if got.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", got.status)
	}
	for _, want := range []string{"Pending orders removed: 3", "4 pending orders, 2 leased orders"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("clear response missing %q: %s", want, got.body)
		}
	}
	if source.calls != 1 {
		t.Fatalf("clear calls = %d, want 1", source.calls)
	}
}

func TestCrawlOrderClearReportsZeroRemovalWithoutFailure(t *testing.T) {
	source := &crawlOrderClearSourceFake{setPendingAfterClear: true}
	options := crawlOrderClearOptions(t, CrawlMonitor{
		QueueAvailable: true,
		QueuePending:   2,
		QueueLeased:    1,
	}, source)
	source.pendingAfterClear = 0
	got := doPost(t, New(options), "/admin/crawl/queue/clear", url.Values{
		"confirmation": {"CLEAR PENDING ORDERS"},
	})
	if got.status != http.StatusOK ||
		!strings.Contains(got.body, "No pending orders were removed.") ||
		!strings.Contains(got.body, "0 pending orders, 1 leased order") ||
		strings.Contains(got.body, "could not be cleared") {
		t.Fatalf("empty clear result = %d %q", got.status, got.body)
	}
	if source.calls != 1 {
		t.Fatalf("clear calls = %d, want 1", source.calls)
	}
}

func TestCrawlOrderClearDoesNotCallSourceWhenQueueDepthIsUnavailable(t *testing.T) {
	source := &crawlOrderClearSourceFake{}
	options := crawlOrderClearOptions(t, CrawlMonitor{
		QueuePending: 3,
		QueueLeased:  1,
	}, source)
	got := doPost(t, New(options), "/admin/crawl/queue/clear", url.Values{
		"confirmation": {"CLEAR PENDING ORDERS"},
	})
	if got.status != http.StatusOK ||
		!strings.Contains(got.body, "Pending order counts are unavailable") {
		t.Fatalf("unavailable queue result = %d %q", got.status, got.body)
	}
	if source.calls != 0 {
		t.Fatalf("clear calls = %d, want 0 for unavailable queue", source.calls)
	}
}

func TestCrawlOrderClearDoesNotCallSourceWhenQueueIsEmpty(t *testing.T) {
	source := &crawlOrderClearSourceFake{}
	options := crawlOrderClearOptions(t, CrawlMonitor{
		QueueAvailable: true,
		QueueLeased:    1,
	}, source)
	got := doPost(t, New(options), "/admin/crawl/queue/clear", url.Values{
		"confirmation": {"CLEAR PENDING ORDERS"},
	})
	if got.status != http.StatusOK ||
		!strings.Contains(got.body, "No pending orders were reported; no clear was started.") {
		t.Fatalf("empty queue result = %d %q", got.status, got.body)
	}
	if source.calls != 0 {
		t.Fatalf("clear calls = %d, want 0 for empty queue", source.calls)
	}
}

func TestCrawlOrderClearPartialFailureHidesBackendDetails(t *testing.T) {
	source := &crawlOrderClearSourceFake{
		removed: 2,
		err:     errors.New("private queue failure at https://private.invalid/?q=sentinel"),
	}
	options := crawlOrderClearOptions(t, CrawlMonitor{
		QueueAvailable: true,
		QueuePending:   4,
		QueueLeased:    1,
	}, source)
	got := doPost(t, New(options), "/admin/crawl/queue/clear", url.Values{
		"confirmation": {"CLEAR PENDING ORDERS"},
	})
	if got.status != http.StatusOK ||
		!strings.Contains(got.body, "Pending orders removed: 2") ||
		!strings.Contains(got.body, "some pending orders may remain") {
		t.Fatalf("partial result = %d %q", got.status, got.body)
	}
	if strings.Contains(got.body, "private queue failure") ||
		strings.Contains(got.body, "private.invalid") ||
		strings.Contains(got.body, "sentinel") {
		t.Fatalf("response exposed backend details: %s", got.body)
	}
}

func TestCrawlOrderClearReportsFailureWhenNoOrdersWereRemoved(t *testing.T) {
	source := &crawlOrderClearSourceFake{
		err: errors.New("private queue failure at https://private.invalid/?q=sentinel"),
	}
	options := crawlOrderClearOptions(t, CrawlMonitor{
		QueueAvailable: true,
		QueuePending:   2,
	}, source)
	got := doPost(t, New(options), "/admin/crawl/queue/clear", url.Values{
		"confirmation": {crawlOrderClearConfirmation},
	})
	const zeroRemovalError = "Pending orders could not be cleared; no orders were removed."
	if got.status != http.StatusOK || !strings.Contains(got.body, zeroRemovalError) {
		t.Fatalf("zero-removal error result = %d %q", got.status, got.body)
	}
	if strings.Contains(got.body, "private queue failure") ||
		strings.Contains(got.body, "private.invalid") ||
		strings.Contains(got.body, "sentinel") {
		t.Fatalf("response exposed backend details: %s", got.body)
	}
	if source.calls != 1 {
		t.Fatalf("clear calls = %d, want 1", source.calls)
	}
}
