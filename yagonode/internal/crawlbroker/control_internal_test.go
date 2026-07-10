package crawlbroker

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func TestControlRegistryEnqueueDrain(t *testing.T) {
	registry := newControlRegistry()
	registry.Enqueue(
		"w1",
		yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlPause},
	)
	registry.Enqueue(
		"w1",
		yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlResume},
	)
	registry.Enqueue(
		"w2",
		yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlCancel},
	)

	w1 := registry.drain("w1")
	if len(w1) != 2 || w1[0].Kind != yagocrawlcontract.CrawlControlPause {
		t.Fatalf("w1 directives = %+v, want pause then resume", w1)
	}
	if drained := registry.drain("w1"); len(drained) != 0 {
		t.Fatalf("second drain = %+v, want empty", drained)
	}
	if w2 := registry.drain("w2"); len(w2) != 1 {
		t.Fatalf("w2 directives = %+v, want one", w2)
	}
}

func TestControlRegistryIgnoresBlankWorker(t *testing.T) {
	registry := newControlRegistry()
	registry.Enqueue(
		"",
		yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlCancel},
	)
	if drained := registry.drain(""); len(drained) != 0 {
		t.Fatalf("blank worker drain = %+v, want empty", drained)
	}
}

func TestDirectivesToProtoMapsFields(t *testing.T) {
	if directivesToProto(nil) != nil {
		t.Fatal("empty directive slice should map to nil")
	}

	kinds := map[yagocrawlcontract.CrawlControlKind]crawlrpc.CrawlControlKind{
		yagocrawlcontract.CrawlControlPause:     crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE,
		yagocrawlcontract.CrawlControlResume:    crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESUME,
		yagocrawlcontract.CrawlControlCancel:    crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL,
		yagocrawlcontract.CrawlControlSetRate:   crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_RATE,
		yagocrawlcontract.CrawlControlRestart:   crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESTART,
		yagocrawlcontract.CrawlControlKind("x"): crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_UNSPECIFIED,
	}
	for kind, want := range kinds {
		if got := controlKindToProto(kind); got != want {
			t.Fatalf("controlKindToProto(%q) = %v, want %v", kind, got, want)
		}
	}
}

func TestDirectiveToProtoDecodesRunID(t *testing.T) {
	proto := directiveToProto(yagocrawlcontract.CrawlControlDirective{
		Kind:           yagocrawlcontract.CrawlControlSetRate,
		RunID:          "abcd",
		PagesPerMinute: 45,
	})
	if hex.EncodeToString(proto.GetRunId()) != "abcd" {
		t.Fatalf("run id = %x, want abcd", proto.GetRunId())
	}
	if proto.GetPagesPerMinute() != 45 {
		t.Fatalf("ppm = %d, want 45", proto.GetPagesPerMinute())
	}
}

func TestDirectiveToProtoMalformedRunIDTargetsWorker(t *testing.T) {
	proto := directiveToProto(yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlCancel,
		RunID: "not-hex",
	})
	if len(proto.GetRunId()) != 0 {
		t.Fatalf("malformed run id decoded to %x, want empty target", proto.GetRunId())
	}
}

func TestExchangeHeartbeatDeliversControlDirectives(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	server.control.Enqueue("w1", yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlCancel,
		RunID: "ab",
	})

	result, err := server.Heartbeat(context.Background(), &crawlrpc.WorkerHeartbeat{WorkerId: "w1"})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if len(result.GetDirectives()) != 1 {
		t.Fatalf("directives = %d, want 1", len(result.GetDirectives()))
	}
	directive := result.GetDirectives()[0]
	if directive.GetKind() != crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL ||
		hex.EncodeToString(directive.GetRunId()) != "ab" {
		t.Fatalf("directive = %+v, want cancel/ab", directive)
	}

	drained, err := server.Heartbeat(
		context.Background(),
		&crawlrpc.WorkerHeartbeat{WorkerId: "w1"},
	)
	if err != nil {
		t.Fatalf("second heartbeat: %v", err)
	}
	if len(drained.GetDirectives()) != 0 {
		t.Fatalf("second heartbeat returned %d directives, want 0", len(drained.GetDirectives()))
	}
}

func TestControlRegistryRestartWorkers(t *testing.T) {
	registry := newControlRegistry()
	if signalled := registry.RestartWorkers(); signalled != 0 {
		t.Fatalf("restart with no workers = %d, want 0", signalled)
	}

	registry.register("w1")
	registry.register("w1") // a second order stream for the same worker
	registry.register("w2")
	registry.register("") // blank id is ignored

	if signalled := registry.RestartWorkers(); signalled != 2 {
		t.Fatalf("restart signalled %d workers, want 2", signalled)
	}
	for _, worker := range []string{"w1", "w2"} {
		drained := registry.drain(worker)
		if len(drained) != 1 || drained[0].Kind != yagocrawlcontract.CrawlControlRestart {
			t.Fatalf("%s drained = %+v, want one restart directive", worker, drained)
		}
	}

	registry.unregister("w1") // still has a second connection
	if signalled := registry.RestartWorkers(); signalled != 2 {
		t.Fatalf("after one unregister = %d, want 2 workers still connected", signalled)
	}
	registry.drain("w1")
	registry.drain("w2")

	registry.unregister("w1") // last connection drops
	registry.unregister("w2")
	registry.unregister("") // blank id is ignored
	if signalled := registry.RestartWorkers(); signalled != 0 {
		t.Fatalf("after all unregister = %d, want 0", signalled)
	}
}

func TestControlRegistryUnregisterReportsLastConnection(t *testing.T) {
	registry := newControlRegistry()
	registry.register("w1")
	registry.register("w1")
	if last := registry.unregister("w1"); last {
		t.Fatal("first unregister of two connections must not report last")
	}
	if last := registry.unregister("w1"); !last {
		t.Fatal("dropping the final connection must report last")
	}
	if last := registry.unregister(""); last {
		t.Fatal("blank worker id reports no connection")
	}
}

func TestStreamOrdersRegistersWorkerForRestart(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if err := queue.Publish(context.Background(), testOrder("reg")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var duringStream int
	stream := &fakeOrderStream{ctx: ctx, onSend: func() {
		duringStream = server.control.RestartWorkers()
		cancel()
	}}
	_ = server.StreamOrders(&crawlrpc.WorkerRegistration{WorkerId: "w1"}, stream)

	if duringStream != 1 {
		t.Fatalf("worker not registered during StreamOrders: restart saw %d", duringStream)
	}
	if after := server.control.RestartWorkers(); after != 0 {
		t.Fatalf("worker still registered after StreamOrders returned: %d", after)
	}
}
