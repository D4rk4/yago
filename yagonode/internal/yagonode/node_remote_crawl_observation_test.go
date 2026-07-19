package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
)

func TestRemoteCrawlDurableEventsExcludeNormalTraffic(t *testing.T) {
	recorder := events.NewRecorder(8)
	observer := remoteCrawlEventObserver{recorder: recorder}
	for _, observation := range []remotecrawl.Observation{
		{Action: "stage", Outcome: "accepted", Count: 1},
		{Action: "lease", Outcome: "accepted", Count: 1},
		{Action: "receipt", Outcome: "accepted", Count: 1},
		{Action: "receipt", Outcome: "requeued", Count: 1},
		{Action: "lease", Outcome: "outstanding_limit"},
	} {
		observer.ObserveRemoteCrawl(observation)
	}
	if recent := recorder.Recent(0); len(recent) != 0 {
		t.Fatalf("normal remote crawl events = %+v", recent)
	}
	for _, observation := range []remotecrawl.Observation{
		{Action: "lease", Outcome: "untrusted"},
		{Action: "lease", Outcome: "rate_limited"},
		{Action: "receipt", Outcome: "metadata_rejected"},
		{Action: "stage", Outcome: "queue_failed"},
		{Action: "receipt", Outcome: "store_requeued"},
	} {
		observer.ObserveRemoteCrawl(observation)
	}
	recent := recorder.Recent(0)
	if len(recent) != 5 {
		t.Fatalf("warning remote crawl events = %+v", recent)
	}
	for _, event := range recent {
		if event.Name != remoteCrawlEventName || event.Severity != events.SeverityWarn {
			t.Fatalf("warning remote crawl event = %+v", event)
		}
	}
}
