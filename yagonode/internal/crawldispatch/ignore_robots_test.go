package crawldispatch_test

import (
	"net/http"
	"testing"
)

// TestDispatchThreadsIgnoreRobotsIntoProfile is the CRAWL-04 leftover: the
// explicit operator opt-out reaches the crawl profile, and its absence leaves
// robots enforced.
func TestDispatchThreadsIgnoreRobotsIntoProfile(t *testing.T) {
	queue := &recordingQueue{}
	mux := mount(t, queue)

	rec := post(t, mux, `{
		"name": "own-site",
		"seeds": ["https://example.org/"],
		"scope": "domain",
		"maxDepth": 1,
		"maxPagesPerHost": 10,
		"ignoreRobots": true
	}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if !queue.order.Profile.IgnoreRobots {
		t.Fatal("ignoreRobots opt-out did not reach the profile")
	}

	rec = post(t, mux, `{
		"name": "default",
		"seeds": ["https://example.org/"],
		"scope": "domain",
		"maxDepth": 1,
		"maxPagesPerHost": 10
	}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if queue.order.Profile.IgnoreRobots {
		t.Fatal("robots must stay enforced by default")
	}
}
