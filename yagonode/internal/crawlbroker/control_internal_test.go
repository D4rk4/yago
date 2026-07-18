package crawlbroker

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func deliveredControls(
	t *testing.T,
	registry *ControlRegistry,
	workerID string,
	acknowledged ...uint64,
) []yagocrawlcontract.CrawlControlDirective {
	t.Helper()
	directives, err := registry.deliverForHeartbeat(t.Context(), workerID, acknowledged)
	if err != nil {
		t.Fatalf("deliver controls: %v", err)
	}

	return directives
}

func controlDirectiveIDs(directives []yagocrawlcontract.CrawlControlDirective) []uint64 {
	identities := make([]uint64, 0, len(directives))
	for _, directive := range directives {
		identities = append(identities, directive.DirectiveID)
	}

	return identities
}

func TestControlRegistryEnqueueDrain(t *testing.T) {
	registry := newControlRegistry()
	registry.register("w1")
	registry.register("w2")
	registry.Enqueue(
		"w1",
		yagocrawlcontract.CrawlControlDirective{
			Kind:  yagocrawlcontract.CrawlControlPause,
			RunID: "01",
		},
	)
	registry.Enqueue(
		"w1",
		yagocrawlcontract.CrawlControlDirective{
			Kind:  yagocrawlcontract.CrawlControlResume,
			RunID: "01",
		},
	)
	registry.Enqueue(
		"w2",
		yagocrawlcontract.CrawlControlDirective{
			Kind:  yagocrawlcontract.CrawlControlCancel,
			RunID: "02",
		},
	)

	w1 := deliveredControls(t, registry, "w1")
	if len(w1) != 2 || w1[0].Kind != yagocrawlcontract.CrawlControlPause {
		t.Fatalf("w1 directives = %+v, want pause then resume", w1)
	}
	if replayed := deliveredControls(t, registry, "w1"); len(replayed) != 2 {
		t.Fatalf("replayed directives = %+v, want both unacknowledged directives", replayed)
	}
	if remaining := deliveredControls(
		t,
		registry,
		"w1",
		controlDirectiveIDs(w1)...); len(
		remaining,
	) != 0 {
		t.Fatalf("acknowledged directives = %+v, want empty", remaining)
	}
	if w2 := deliveredControls(t, registry, "w2"); len(w2) != 1 {
		t.Fatalf("w2 directives = %+v, want one", w2)
	}
}

func TestControlRegistryIgnoresBlankWorker(t *testing.T) {
	registry := newControlRegistry()
	registry.Enqueue(
		"",
		yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlCancel},
	)
	if drained := deliveredControls(t, registry, ""); len(drained) != 0 {
		t.Fatalf("blank worker drain = %+v, want empty", drained)
	}
}

func TestControlRegistryRetainsTargetedDirectivesAcrossOfflineWorker(t *testing.T) {
	registry := newControlRegistry()
	directive := yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlPause,
		RunID: "01",
	}
	if !registry.Enqueue("offline", directive) {
		t.Fatal("offline worker rejected a run directive")
	}
	if registry.Enqueue("offline", yagocrawlcontract.CrawlControlDirective{
		Kind: yagocrawlcontract.CrawlControlRestart,
	}) {
		t.Fatal("offline worker accepted a worker-wide directive")
	}
	registry.register("offline")
	if replayed := deliveredControls(t, registry, "offline"); len(replayed) != 1 ||
		replayed[0].Kind != yagocrawlcontract.CrawlControlPause {
		t.Fatalf("offline worker replay = %v, want pause", replayed)
	}
	registry.register("worker")
	if !registry.Enqueue("worker", directive) {
		t.Fatal("connected worker rejected a control directive")
	}
	registry.unregister("worker")
	if replayed := deliveredControls(t, registry, "worker"); len(replayed) != 1 ||
		replayed[0].Kind != yagocrawlcontract.CrawlControlPause {
		t.Fatalf("disconnected worker replay = %v, want pause", replayed)
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
		yagocrawlcontract.CrawlControlSetActiveRuns:                 crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_ACTIVE_RUNS,
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
		MaximumActiveRuns:            37,
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
	if proto.GetMaximumActiveRuns() != 37 {
		t.Fatalf("maximum active runs = %d, want 37", proto.GetMaximumActiveRuns())
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
	initial := deliveredControls(t, registry, "w1")
	if len(initial) != 2 || initial[0].Kind != yagocrawlcontract.CrawlControlSetWorkers ||
		initial[0].FetchWorkers != 4 ||
		initial[1].Kind != yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority ||
		initial[1].PrioritizeAutomaticDiscovery {
		t.Fatalf("initial directives = %+v, want set_workers/4 and disabled priority", initial)
	}
	deliveredControls(t, registry, "w1", controlDirectiveIDs(initial)...)
	registry.register("w2")
	w2Initial := deliveredControls(t, registry, "w2")
	deliveredControls(t, registry, "w2", controlDirectiveIDs(w2Initial)...)
	if signalled := registry.SetFetchWorkers(12); signalled != 2 {
		t.Fatalf("set workers signalled %d workers, want 2", signalled)
	}
	if signalled := registry.SetAutomaticDiscoveryPriority(true); signalled != 2 {
		t.Fatalf("set priority signalled %d workers, want 2", signalled)
	}
	for _, worker := range []string{"w1", "w2"} {
		directives := deliveredControls(t, registry, worker)
		if len(directives) != 2 || directives[0].FetchWorkers != 12 ||
			!directives[1].PrioritizeAutomaticDiscovery {
			t.Fatalf(
				"%s directives = %+v, want workers/12 and enabled priority",
				worker,
				directives,
			)
		}
		deliveredControls(t, registry, worker, controlDirectiveIDs(directives)...)
	}
	registry.unregister("w1")
	registry.register("w1")
	reconnected := deliveredControls(t, registry, "w1")
	if len(reconnected) != 2 || reconnected[0].FetchWorkers != 12 ||
		!reconnected[1].PrioritizeAutomaticDiscovery {
		t.Fatalf("reconnected directives = %+v, want workers/12 and enabled priority", reconnected)
	}
	if signalled := registry.SetFetchWorkers(0); signalled != 0 {
		t.Fatalf("invalid worker limit signalled %d workers", signalled)
	}
}

func TestRegisteredSessionHeartbeatReceivesAuthoritativeCrawlerDefaults(t *testing.T) {
	server := newExchangeServer(
		memQueue(t),
		make(chan crawlresults.IngestDelivery),
		crawlerControlDefaults{fetchWorkers: 7, prioritizeAutomaticDiscovery: false},
	)
	activateTestWorkerSession(t, server, "starting-worker", testWorkerSessionID)

	result, err := server.Heartbeat(
		context.Background(),
		&crawlrpc.WorkerHeartbeat{
			WorkerId: "starting-worker", WorkerSessionId: testWorkerSessionID,
		},
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
	if directives[0].GetDirectiveId() == 0 || directives[1].GetDirectiveId() == 0 {
		t.Fatalf("startup directive identities = %+v, want nonzero", directives)
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
	activateTestWorkerSession(t, server, "w1", testWorkerSessionID)
	server.control.register("w1")
	server.control.Enqueue("w1", yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlCancel,
		RunID: "ab",
	})

	result, err := server.Heartbeat(context.Background(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
	})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if len(result.GetDirectives()) != 1 {
		t.Fatalf("directives = %d, want 1", len(result.GetDirectives()))
	}
	directive := result.GetDirectives()[0]
	if directive.GetKind() != crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL ||
		hex.EncodeToString(directive.GetRunId()) != "ab" || directive.GetDirectiveId() == 0 {
		t.Fatalf("directive = %+v, want cancel/ab", directive)
	}

	replayed, err := server.Heartbeat(
		context.Background(),
		&crawlrpc.WorkerHeartbeat{WorkerId: "w1", WorkerSessionId: testWorkerSessionID},
	)
	if err != nil {
		t.Fatalf("second heartbeat: %v", err)
	}
	if len(replayed.GetDirectives()) != 1 ||
		replayed.GetDirectives()[0].GetDirectiveId() != directive.GetDirectiveId() {
		t.Fatalf(
			"second heartbeat directives = %+v, want same unacknowledged directive",
			replayed.GetDirectives(),
		)
	}

	drained, err := server.Heartbeat(
		context.Background(),
		&crawlrpc.WorkerHeartbeat{
			WorkerId:                 "w1",
			WorkerSessionId:          testWorkerSessionID,
			AcknowledgedDirectiveIds: []uint64{directive.GetDirectiveId()},
		},
	)
	if err != nil {
		t.Fatalf("acknowledging heartbeat: %v", err)
	}
	if len(drained.GetDirectives()) != 0 {
		t.Fatalf(
			"acknowledging heartbeat returned %d directives, want 0",
			len(drained.GetDirectives()),
		)
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
		drained := deliveredControls(t, registry, worker)
		if len(drained) != 1 || drained[0].Kind != yagocrawlcontract.CrawlControlRestart {
			t.Fatalf("%s drained = %+v, want one restart directive", worker, drained)
		}
		deliveredControls(t, registry, worker, controlDirectiveIDs(drained)...)
	}

	registry.unregister("w1") // still has a second connection
	if signalled := registry.RestartWorkers(); signalled != 2 {
		t.Fatalf("after one unregister = %d, want 2 workers still connected", signalled)
	}
	w1Restart := deliveredControls(t, registry, "w1")
	deliveredControls(t, registry, "w1", controlDirectiveIDs(w1Restart)...)
	w2Restart := deliveredControls(t, registry, "w2")
	deliveredControls(t, registry, "w2", controlDirectiveIDs(w2Restart)...)

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
	_ = server.StreamOrders(&crawlrpc.WorkerRegistration{
		WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
	}, stream)

	if duringStream != 1 {
		t.Fatalf("worker not registered during StreamOrders: restart saw %d", duringStream)
	}
	if after := server.control.RestartWorkers(); after != 0 {
		t.Fatalf("worker still registered after StreamOrders returned: %d", after)
	}
}
