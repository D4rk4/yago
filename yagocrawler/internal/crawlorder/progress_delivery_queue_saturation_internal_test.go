package crawlorder

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func TestProgressDeliveryCapacityProtectsSeparatingRunningTail(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan *crawlrpc.CrawlProgressReport, 4)
	client := &progressDeliveryClient{gate: gate, started: started}
	policy := testProgressDeliveryPolicy()
	policy.capacity = 2
	queue := newProgressDeliveryQueue(client, "worker-protected", policy)
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("repeated"), State: yagocrawlcontract.CrawlRunCancelled,
	})
	waitProgressCall(t, started)
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("repeated"), State: yagocrawlcontract.CrawlRunRunning,
	})
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("repeated"), State: yagocrawlcontract.CrawlRunFinished,
	})
	queue.mu.Lock()
	sequence := append([]progressDelivery(nil), queue.pending["repeated"]...)
	queued := queue.queued
	queue.mu.Unlock()
	if len(sequence) != 2 || queued != 2 || !sequence[0].terminal || sequence[1].terminal {
		t.Fatalf("saturated repeated sequence = queued:%d %#v", queued, sequence)
	}
	close(gate)
	second := waitProgressCall(t, started)
	if second.GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING {
		t.Fatalf("second saturated report = %+v", second)
	}
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close queue: %v", err)
	}
	calls, _ := client.snapshot()
	if len(calls) != 2 ||
		calls[0].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED ||
		calls[1].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING {
		t.Fatalf("saturated protected calls = %+v", calls)
	}
	queue.mu.Lock()
	closedVersion := queue.version
	queue.mu.Unlock()
	queue.enqueue(t.Context(), RunReport{
		Provenance: []byte("closed"), State: yagocrawlcontract.CrawlRunFinished,
	})
	queue.mu.Lock()
	versionAfterEnqueue := queue.version
	queue.mu.Unlock()
	if versionAfterEnqueue != closedVersion {
		t.Fatalf("closed queue version = %d, want %d", versionAfterEnqueue, closedVersion)
	}
}

func TestProgressDeliveryCapacityEvictsAnotherRunsExpendableTail(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan *crawlrpc.CrawlProgressReport, 4)
	client := &progressDeliveryClient{gate: gate, started: started}
	policy := testProgressDeliveryPolicy()
	policy.capacity = 3
	queue := newProgressDeliveryQueue(client, "worker-preferred-eviction", policy)
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("repeated"), State: yagocrawlcontract.CrawlRunCancelled,
	})
	waitProgressCall(t, started)
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("repeated"), State: yagocrawlcontract.CrawlRunRunning,
	})
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("expendable"), State: yagocrawlcontract.CrawlRunRunning,
	})
	queue.enqueue(context.Background(), RunReport{
		Provenance: []byte("repeated"), State: yagocrawlcontract.CrawlRunFinished,
	})
	queue.mu.Lock()
	sequence := append([]progressDelivery(nil), queue.pending["repeated"]...)
	_, expendable := queue.pending["expendable"]
	queued := queue.queued
	queue.mu.Unlock()
	if len(sequence) != 3 || queued != 3 || expendable ||
		!sequence[0].terminal || sequence[1].terminal || !sequence[2].terminal {
		t.Fatalf("preferred eviction = queued:%d expendable:%t %#v",
			queued, expendable, sequence)
	}
	close(gate)
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close queue: %v", err)
	}
	calls, _ := client.snapshot()
	if len(calls) != 3 ||
		calls[0].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_CANCELLED ||
		calls[1].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING ||
		calls[2].GetState() != crawlrpc.CrawlRunState_CRAWL_RUN_STATE_FINISHED {
		t.Fatalf("preserved transitions = %+v", calls)
	}
}
