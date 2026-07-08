package crawldispatch_test

import (
	"net/http"
	"testing"
)

// TestDispatchThreadsNoindexCanonicalMismatchIntoProfile is the CRAWL-29
// operator surface: the opt-in reaches the crawl profile, and its absence
// keeps canonical-mismatching pages indexed.
func TestDispatchThreadsNoindexCanonicalMismatchIntoProfile(t *testing.T) {
	queue := &recordingQueue{}
	mux := mount(t, queue)

	rec := post(t, mux, `{
		"name": "canonical-strict",
		"seeds": ["https://example.org/"],
		"scope": "domain",
		"maxDepth": 1,
		"maxPagesPerHost": 10,
		"noindexCanonicalMismatch": true
	}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if !queue.order.Profile.NoindexCanonicalMismatch {
		t.Fatal("noindexCanonicalMismatch opt-in did not reach the profile")
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
	if queue.order.Profile.NoindexCanonicalMismatch {
		t.Fatal("canonical-mismatch pages must stay indexed by default")
	}
}
