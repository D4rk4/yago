package crawlorder

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestProgressDeliveryRetiresImmutableRunningAttemptBeforeFairReplacement(t *testing.T) {
	for _, test := range []struct {
		name     string
		firstErr error
	}{
		{name: "completed"},
		{name: "failed", firstErr: errors.New("running delivery failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			gate := make(chan struct{})
			started := make(chan *crawlrpc.CrawlProgressReport, 4)
			client := &progressDeliveryClient{gate: gate, started: started}
			if test.firstErr != nil {
				client.errors = []error{test.firstErr}
			}
			queue := newProgressDeliveryQueue(
				client,
				"worker-fair-running",
				testProgressDeliveryPolicy(),
			)
			enqueueRunningProgress(queue, "busy", 1)
			first := waitProgressCall(t, started)
			if first.GetTally().GetIndexed() != 1 {
				t.Fatalf("first running snapshot = %+v", first)
			}
			enqueueRunningProgress(queue, "busy", 2)
			enqueueRunningProgress(queue, "busy", 3)
			enqueueRunningProgress(queue, "waiting", 1)
			queue.mu.Lock()
			busy := append([]progressDelivery(nil), queue.pending["busy"]...)
			queued := queue.queued
			queue.mu.Unlock()
			if len(busy) != 2 || queued != 3 ||
				busy[0].report.Tally.Indexed != 1 || busy[1].report.Tally.Indexed != 3 {
				t.Fatalf("immutable running sequence = queued:%d %#v", queued, busy)
			}
			close(gate)
			second := waitProgressCall(t, started)
			third := waitProgressCall(t, started)
			if string(second.GetRunId()) != "waiting" ||
				string(third.GetRunId()) != "busy" || third.GetTally().GetIndexed() != 3 {
				t.Fatalf("fair running order = second:%+v third:%+v", second, third)
			}
			if err := queue.close(t.Context()); err != nil {
				t.Fatalf("close fair running queue: %v", err)
			}
		})
	}
}

func TestProgressDeliveryCapacityEvictsPendingReplacementNotInFlightAttempt(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan *crawlrpc.CrawlProgressReport, 3)
	client := &progressDeliveryClient{gate: gate, started: started}
	policy := testProgressDeliveryPolicy()
	policy.capacity = 2
	queue := newProgressDeliveryQueue(client, "worker-in-flight-capacity", policy)
	enqueueRunningProgress(queue, "busy", 1)
	waitProgressCall(t, started)
	enqueueRunningProgress(queue, "busy", 2)
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("terminal"), State: yagocrawlcontract.CrawlRunFinished,
	})
	queue.mu.Lock()
	busy := append([]progressDelivery(nil), queue.pending["busy"]...)
	terminal := append([]progressDelivery(nil), queue.pending["terminal"]...)
	queued := queue.queued
	queue.mu.Unlock()
	if len(busy) != 1 || busy[0].report.Tally.Indexed != 1 ||
		len(terminal) != 1 || !terminal[0].terminal || queued != 2 {
		t.Fatalf(
			"capacity retained busy/terminal/queued = %#v/%#v/%d",
			busy,
			terminal,
			queued,
		)
	}
	close(gate)
	second := waitProgressCall(t, started)
	if string(second.GetRunId()) != "terminal" {
		t.Fatalf("post-capacity report = %+v", second)
	}
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close capacity queue: %v", err)
	}
}

func enqueueRunningProgress(queue *progressDeliveryQueue, runID string, indexed uint64) {
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte(runID),
		State:      yagocrawlcontract.CrawlRunRunning,
		Tally:      yagocrawlcontract.CrawlRunTally{Indexed: indexed},
	})
}
