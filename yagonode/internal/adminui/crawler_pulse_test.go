package adminui

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fixedCrawlerPulse struct {
	pulse CrawlerPulse
}

func (f fixedCrawlerPulse) CrawlerPulse(context.Context) CrawlerPulse {
	return f.pulse
}

func TestCrawlerPulseQueueHintsRequireQueuedOrders(t *testing.T) {
	cases := []struct {
		name  string
		pulse CrawlerPulse
		want  string
		avoid string
	}{
		{
			name:  "queued orders without workers",
			pulse: CrawlerPulse{Available: true, QueueKnown: true, QueuePending: 3},
			want:  "queued with no connected crawler workers",
		},
		{
			name:  "healthy empty queue",
			pulse: CrawlerPulse{Available: true, QueueKnown: true},
			avoid: "queued with no connected crawler workers",
		},
		{
			name: "unknown queue does not diagnose workers",
			pulse: CrawlerPulse{
				Available: true,
				Workers:   []CrawlerWorkerPulse{measuredCrawlerWorker(16, 0)},
			},
			avoid: "fetch slots are occupied",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assertCrawlerPulseHint(t, testCase.pulse, testCase.want, testCase.avoid)
		})
	}
}

func TestCrawlerPulseCapacityHintsRequireCompleteEvidence(t *testing.T) {
	cases := []struct {
		name  string
		pulse CrawlerPulse
		want  string
		avoid string
	}{
		{
			name:  "permit wait is dominant",
			pulse: queuedCrawlerPulse(measuredCrawlerWorker(10, 9)),
			want:  "waiting for a start permit",
		},
		{
			name:  "occupied fetch slots",
			pulse: queuedCrawlerPulse(measuredCrawlerWorker(16, 0)),
			want:  "all reported fetch slots are occupied",
		},
		{
			name:  "idle fetch slots",
			pulse: queuedCrawlerPulse(measuredCrawlerWorker(2, 0)),
			want:  "most reported fetch slots are idle",
		},
		{
			name:  "unknown activity does not diagnose capacity",
			pulse: queuedCrawlerPulse(CrawlerWorkerPulse{WorkerID: "worker-a", FetchLimit: 16}),
			avoid: "fetch slots are occupied",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assertCrawlerPulseHint(t, testCase.pulse, testCase.want, testCase.avoid)
		})
	}
}

func assertCrawlerPulseHint(t *testing.T, pulse CrawlerPulse, want, avoid string) {
	t.Helper()
	view := buildCrawlerPulseView(
		pulse,
		time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC),
	)
	joined := strings.Join(view.Hints, " ")
	if want != "" && !strings.Contains(joined, want) {
		t.Fatalf("hints = %q, want %q", joined, want)
	}
	if avoid != "" && strings.Contains(joined, avoid) {
		t.Fatalf("hints = %q, must not claim %q", joined, avoid)
	}
}

func measuredCrawlerWorker(active, waiting int) CrawlerWorkerPulse {
	return CrawlerWorkerPulse{
		WorkerID:                   "worker-a",
		ActiveFetches:              active,
		ActiveFetchesKnown:         true,
		FetchLimit:                 16,
		FetchAdmissionWaiting:      waiting,
		FetchAdmissionWaitingKnown: true,
	}
}

func queuedCrawlerPulse(worker CrawlerWorkerPulse) CrawlerPulse {
	return CrawlerPulse{
		Available:    true,
		QueueKnown:   true,
		QueuePending: 2,
		Workers:      []CrawlerWorkerPulse{worker},
	}
}

func TestCrawlerPulseUnavailableAndUnknownQueueStates(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	unavailable := buildCrawlerPulseView(CrawlerPulse{
		QueueKnown:   true,
		QueuePending: 3,
		Workers: []CrawlerWorkerPulse{{
			WorkerID: "worker-a", ActiveFetches: 2, ActiveFetchesKnown: true,
		}},
	}, now)
	if unavailable.Refresh || unavailable.Workers != 0 || unavailable.QueuePending != "" ||
		len(unavailable.WorkerRows) != 0 {
		t.Fatalf("unavailable crawler view = %+v, want no stale measurements", unavailable)
	}
	unknownQueue := buildCrawlerPulseView(CrawlerPulse{Available: true}, now)
	if unknownQueue.QueuePending != "Unavailable" || unknownQueue.Workers != 0 {
		t.Fatalf("unknown queue view = %+v, want unavailable queue and zero workers", unknownQueue)
	}
}

func TestCrawlerPulseReportsPartialRatesAndClampsFutureReportTime(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	pulse := CrawlerPulse{
		Available: true,
		Workers: []CrawlerWorkerPulse{
			{
				WorkerID:              "worker-measured",
				ProcessLabel:          "session-one",
				ActiveFetches:         1,
				ActiveFetchesKnown:    true,
				FetchedPagesPerMinute: 12,
				IndexedPagesPerMinute: 6,
				RateKnown:             true,
				ObservedAt:            now.Add(time.Second),
			},
			{WorkerID: "worker-collecting", ActiveFetchesKnown: true, ActiveFetches: 1},
		},
	}
	view := buildCrawlerPulseView(pulse, now)
	if view.Fetched != "12.0 (1/2 workers reporting)" ||
		view.Indexed != "6.0 (1/2 workers reporting)" {
		t.Fatalf("partial fleet rates = %s/%s", view.Fetched, view.Indexed)
	}
	if view.WorkerRows[0].LastReport != "0s ago" {
		t.Fatalf("future report age = %q, want clamped zero age", view.WorkerRows[0].LastReport)
	}
}

func TestCrawlerPulseDistinguishesIdleFromUnavailableAndWarmup(t *testing.T) {
	now := time.Now()
	empty := buildCrawlerPulseView(CrawlerPulse{Available: true}, now)
	if empty.Fetched != "Unavailable" || empty.Indexed != "Unavailable" {
		t.Fatalf("empty fleet rates = %s/%s, want unavailable", empty.Fetched, empty.Indexed)
	}
	idle := buildCrawlerPulseView(CrawlerPulse{
		Available: true,
		Workers: []CrawlerWorkerPulse{{
			WorkerID: "worker-idle", ActiveFetchesKnown: true,
		}},
	}, now)
	if idle.Fetched != "Idle" || idle.WorkerRows[0].Fetched != "Idle" {
		t.Fatalf("idle fleet rates = %s/%s, want idle", idle.Fetched, idle.WorkerRows[0].Fetched)
	}
	if idle.WorkerRows[0].ProcessLabel != "Unavailable" {
		t.Fatalf(
			"idle process label = %q, want unavailable before a heartbeat",
			idle.WorkerRows[0].ProcessLabel,
		)
	}
	busy := buildCrawlerPulseView(CrawlerPulse{
		Available: true,
		Workers: []CrawlerWorkerPulse{{
			WorkerID: "worker-busy", ActiveFetches: 1, ActiveFetchesKnown: true,
		}},
	}, now)
	if busy.Fetched != "Collecting" || busy.WorkerRows[0].Fetched != "Collecting" {
		t.Fatalf(
			"newly busy fleet rates = %s/%s, want collecting",
			busy.Fetched,
			busy.WorkerRows[0].Fetched,
		)
	}
}

func TestPerformancePageRendersCrawlerPulseAndMeasuredRates(t *testing.T) {
	now := time.Now()
	console := New(Options{
		Performance: fakePerformance{snap: PerformanceStatus{Available: true}},
		CrawlerPulse: fixedCrawlerPulse{pulse: CrawlerPulse{
			Available:    true,
			QueueKnown:   true,
			QueuePending: 2,
			Workers: []CrawlerWorkerPulse{{
				WorkerID:                   "worker-test",
				ProcessLabel:               "a1b2c3d4",
				ActiveFetches:              3,
				ActiveFetchesKnown:         true,
				FetchLimit:                 8,
				FetchAdmissionWaiting:      1,
				FetchAdmissionWaitingKnown: true,
				FetchedPagesPerMinute:      24.5,
				IndexedPagesPerMinute:      20,
				RateKnown:                  true,
				ObservedAt:                 now.Add(-5 * time.Second),
			}},
		}},
	})
	response := do(t, console, "/admin/performance")
	for _, want := range []string{"Crawler pulse", "Fetched pages per minute", "Indexed pages per minute", "worker-test", "a1b2c3d4", "Process ID", "3 / 8", "24.5", "20.0", "Pending URLs remain"} {
		if !strings.Contains(response.body, want) {
			t.Errorf("performance page missing %q", want)
		}
	}
	if !strings.Contains(response.body, `hx-trigger="every 5s"`) ||
		!strings.Contains(response.body, `hx-get="/admin/performance/crawler-pulse"`) {
		t.Fatal("crawler pulse does not refresh from the typed server-rendered fragment")
	}
	fragment := do(t, console, crawlerPulsePath)
	if !strings.Contains(fragment.body, `id="crawler-pulse"`) ||
		!strings.Contains(fragment.body, "worker-test") {
		t.Fatalf("crawler pulse refresh fragment = %s", fragment.body)
	}
}
