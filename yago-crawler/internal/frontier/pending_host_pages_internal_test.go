package frontier

import (
	"fmt"
	"testing"
)

func TestPendingHostsCompactAfterManyOriginsDrain(t *testing.T) {
	run := &crawlRun{pendingByHost: make(map[string]*pendingHostPages)}
	const hosts = 512
	for index := range hosts {
		run.appendPending(frontierCandidate{
			normURL: fmt.Sprintf("https://host-%03d.example/page", index),
			host:    fmt.Sprintf("host-%03d.example", index),
		})
	}
	for range hosts / 2 {
		if _, _, ok := run.popPending(nil); !ok {
			t.Fatal("pending host drained before half the origins")
		}
	}
	if len(run.pendingHosts) != hosts/2 || run.pendingHostLive != hosts/2 {
		t.Fatalf(
			"compacted hosts = %d/%d, want %d/%d",
			len(run.pendingHosts),
			run.pendingHostLive,
			hosts/2,
			hosts/2,
		)
	}
	for slot, bucket := range run.pendingHosts {
		if bucket == nil || bucket.slot != slot {
			t.Fatalf("compacted host slot %d = %#v", slot, bucket)
		}
	}
}

func TestPendingHostProbeSkipsTombstonesWithoutConsumingRejectedPages(t *testing.T) {
	run := &crawlRun{pendingByHost: make(map[string]*pendingHostPages)}
	for _, host := range []string{"first.example", "second.example", "third.example"} {
		run.appendPending(frontierCandidate{normURL: "https://" + host + "/", host: host})
	}
	if _, _, ok := run.popPending(nil); !ok {
		t.Fatal("first pending host was not available")
	}
	run.pendingCursor = 0
	if _, _, ok := run.popPending(func(string, pendingPage) bool { return false }); ok {
		t.Fatal("rejected host probe consumed a page")
	}
	if run.pendingPages != 2 {
		t.Fatalf("pending pages after rejected probe = %d, want 2", run.pendingPages)
	}
}

func TestReturnedHostPagesPrecedeQueuedPages(t *testing.T) {
	run := &crawlRun{pendingByHost: make(map[string]*pendingHostPages)}
	run.appendPending(frontierCandidate{normURL: "https://example.org/queued", host: "example.org"})
	run.prependReturned("example.org", []pendingPage{{normURL: "https://example.org/returned"}})
	_, page, ok := run.popPending(nil)
	if !ok || page.normURL != "https://example.org/returned" {
		t.Fatalf("first pending page = %q, %v", page.normURL, ok)
	}
	_, page, ok = run.popPending(nil)
	if !ok || page.normURL != "https://example.org/queued" {
		t.Fatalf("second pending page = %q, %v", page.normURL, ok)
	}
}
