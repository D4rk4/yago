package crawldispatch_test

import (
	"net/http"
	"testing"
	"time"
)

// TestDispatchParsesFriendlyRecrawlInterval proves the operator crawl request
// accepts the day/off recrawl spellings and threads the parsed interval onto
// the crawl profile's RecrawlIfOlder, so a page is re-fetched on schedule.
func TestDispatchParsesFriendlyRecrawlInterval(t *testing.T) {
	queue := &recordingQueue{}
	mux := mount(t, queue)

	rec := post(t, mux, `{
		"name": "cadence",
		"seeds": ["https://example.org/"],
		"scope": "domain",
		"maxDepth": 1,
		"maxPagesPerHost": 10,
		"recrawlIfOlder": "30d"
	}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if queue.order.Profile.RecrawlIfOlder != 30*24*time.Hour {
		t.Fatalf("RecrawlIfOlder = %v, want 720h", queue.order.Profile.RecrawlIfOlder)
	}

	rec = post(t, mux, `{
		"name": "disabled",
		"seeds": ["https://example.org/"],
		"scope": "domain",
		"maxDepth": 1,
		"maxPagesPerHost": 10,
		"recrawlIfOlder": "off"
	}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if queue.order.Profile.RecrawlIfOlder != 0 {
		t.Fatalf("off RecrawlIfOlder = %v, want 0", queue.order.Profile.RecrawlIfOlder)
	}
}

// TestDispatchRejectsInvalidRecrawlInterval proves a malformed recrawl cadence
// is a 400, not a silently dropped field.
func TestDispatchRejectsInvalidRecrawlInterval(t *testing.T) {
	mux := mount(t, &recordingQueue{})

	rec := post(t, mux, `{
		"name": "bad",
		"seeds": ["https://example.org/"],
		"scope": "domain",
		"maxDepth": 1,
		"maxPagesPerHost": 10,
		"recrawlIfOlder": "nonsense"
	}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
