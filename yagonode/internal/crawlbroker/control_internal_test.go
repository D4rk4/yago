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
	registry.register("w1")
	registry.register("w2")
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

func TestControlRegistryRejectsOfflineWorkerAndDropsPendingOnDisconnect(t *testing.T) {
	registry := newControlRegistry()
	directive := yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlPause}
	if registry.Enqueue("offline", directive) {
		t.Fatal("offline worker accepted a control directive")
	}
	registry.register("worker")
	if !registry.Enqueue("worker", directive) {
		t.Fatal("connected worker rejected a control directive")
	}
	registry.unregister("worker")
	if drained := registry.drain("worker"); len(drained) != 0 {
		t.Fatalf("disconnected worker retained directives: %v", drained)
	}
}

func TestDirectivesToProtoMapsFields(t *testing.T) {
	if directivesToProto(nil) != nil {
		t.Fatal("empty directive slice should map to nil")
	}

	kinds := map[yagocrawlcontract.CrawlControlKind]crawlrpc.CrawlControlKind{
		yagocrawlcontract.CrawlControlPause:                         crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE,
		yagocrawlcontract.CrawlControlResume:                        crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESUME,
		yagocrawlcontract.CrawlControlCancel:                        crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL,
		yagocrawlcontract.CrawlControlSetRate:                       crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_RATE,
		yagocrawlcontract.CrawlControlRestart:                       crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESTART,
		yagocrawlcontract.CrawlControlSetWorkers:                    crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_WORKERS,
		yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority: crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_AUTOMATIC_DISCOVERY_PRIORITY,
		yagocrawlcontract.CrawlControlKind("x"):                     crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_UNSPECIFIED,
	}
	for kind, want := range kinds {
		if got := controlKindToProto(kind); got != want {
			t.Fatalf("controlKindToProto(%q) = %v, want %v", kind, got, want)
		}
	}
}

func TestDirectiveToProtoDecodesRunID(t *testing.T) {
	proto := directiveToProto(yagocrawlcontract.CrawlControlDirective{
		Kind:                         yagocrawlcontract.CrawlControlSetRate,
		RunID:                        "abcd",
		PagesPerMinute:               45,
		FetchWorkers:                 9,
		PrioritizeAutomaticDiscovery: true,
	})
	if hex.EncodeToString(proto.GetRunId()) != "abcd" {
		t.Fatalf("run id = %x, want abcd", proto.GetRunId())
	}
	if proto.GetPagesPerMinute() != 45 {
		t.Fatalf("ppm = %d, want 45", proto.GetPagesPerMinute())
	}
	if proto.GetFetchWorkers() != 9 {
		t.Fatalf("fetch workers = %d, want 9", proto.GetFetchWorkers())
	}
	if !proto.GetPrioritizeAutomaticDiscovery() {
		t.Fatal("automatic discovery priority = false, want true")
	}
}

func TestControlRegistryConvergesConnectedAndReconnectedWorkers(t *testing.T) {
	registry := newControlRegistry(crawlerControlDefaults{
		fetchWorkers:                 4,
		prioritizeAutomaticDiscovery: false,
	})
	registry.register("w1")
	initial := registry.drain("w1")
	if len(initial) != 2 || initial[0].Kind != yagocrawlcontract.CrawlControlSetWorkers ||
		initial[0].FetchWorkers != 4 ||
		initial[1].Kind != yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority ||
		initial[1].PrioritizeAutomaticDiscovery {
		t.Fatalf("initial directives = %+v, want set_workers/4 and disabled priority", initial)
	}
	registry.register("w2")
	registry.drain("w2")
	if signalled := registry.SetFetchWorkers(12); signalled != 2 {
		t.Fatalf("set workers signalled %d workers, want 2", signalled)
	}
	if signalled := registry.SetAutomaticDiscoveryPriority(true); signalled != 2 {
		t.Fatalf("set priority signalled %d workers, want 2", signalled)
	}
	for _, worker := range []string{"w1", "w2"} {
		directives := registry.drain(worker)
		if len(directives) != 2 || directives[0].FetchWorkers != 12 ||
			!directives[1].PrioritizeAutomaticDiscovery {
			t.Fatalf(
				"%s directives = %+v, want workers/12 and enabled priority",
				worker,
				directives,
			)
		}
	}
	registry.unregister("w1")
	registry.register("w1")
	reconnected := registry.drain("w1")
	if len(reconnected) != 2 || reconnected[0].FetchWorkers != 12 ||
		!reconnected[1].PrioritizeAutomaticDiscovery {
		t.Fatalf("reconnected directives = %+v, want workers/12 and enabled priority", reconnected)
	}
	if signalled := registry.SetFetchWorkers(0); signalled != 0 {
		t.Fatalf("invalid worker limit signalled %d workers", signalled)
	}
}

func TestUnregisteredHeartbeatReceivesAuthoritativeCrawlerDefaults(t *testing.T) {
	server := newExchangeServer(
		memQueue(t),
		make(chan crawlresults.IngestDelivery),
		crawlerControlDefaults{fetchWorkers: 7, prioritizeAutomaticDiscovery: false},
	)

	result, err := server.Heartbeat(
		context.Background(),
		&crawlrpc.WorkerHeartbeat{WorkerId: "starting-worker"},
	)
	if err != nil {
		t.Fatalf("startup heartbeat: %v", err)
	}
	directives := result.GetDirectives()
	if len(directives) != 2 || directives[0].GetFetchWorkers() != 7 ||
		directives[1].GetKind() != crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_AUTOMATIC_DISCOVERY_PRIORITY ||
		directives[1].GetPrioritizeAutomaticDiscovery() {
		t.Fatalf("startup directives = %+v, want workers/7 and disabled priority", directives)
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
	server.control.register("w1")
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
