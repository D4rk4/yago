package crawlorder

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type recordingControlHandler struct {
	mu      sync.Mutex
	applied []yagocrawlcontract.CrawlControlDirective
}

func (h *recordingControlHandler) Apply(
	_ context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.applied = append(h.applied, directive)
}

func (h *recordingControlHandler) snapshot() []yagocrawlcontract.CrawlControlDirective {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]yagocrawlcontract.CrawlControlDirective(nil), h.applied...)
}

func TestDirectiveFromProtoMapsKinds(t *testing.T) {
	cases := map[crawlrpc.CrawlControlKind]yagocrawlcontract.CrawlControlKind{
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE:                            yagocrawlcontract.CrawlControlPause,
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESUME:                           yagocrawlcontract.CrawlControlResume,
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL:                           yagocrawlcontract.CrawlControlCancel,
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_RATE:                         yagocrawlcontract.CrawlControlSetRate,
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESTART:                          yagocrawlcontract.CrawlControlRestart,
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_WORKERS:                      yagocrawlcontract.CrawlControlSetWorkers,
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_AUTOMATIC_DISCOVERY_PRIORITY: yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority,
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_UNSPECIFIED: yagocrawlcontract.CrawlControlKind(
			"",
		),
	}
	for proto, want := range cases {
		if got := controlKindFromProto(proto); got != want {
			t.Fatalf("controlKindFromProto(%v) = %q, want %q", proto, got, want)
		}
	}
}

func TestDirectiveFromProtoEncodesRunID(t *testing.T) {
	directive := directiveFromProto(&crawlrpc.CrawlControlDirective{
		DirectiveId:                  29,
		Kind:                         crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_RATE,
		RunId:                        []byte{0xab, 0xcd},
		PagesPerMinute:               30,
		FetchWorkers:                 7,
		PrioritizeAutomaticDiscovery: true,
	})
	if directive.Kind != yagocrawlcontract.CrawlControlSetRate {
		t.Fatalf("kind = %q", directive.Kind)
	}
	if directive.DirectiveID != 29 {
		t.Fatalf("directive id = %d, want 29", directive.DirectiveID)
	}
	if directive.RunID != "abcd" {
		t.Fatalf("run id = %q, want abcd", directive.RunID)
	}
	if directive.PagesPerMinute != 30 {
		t.Fatalf("ppm = %d, want 30", directive.PagesPerMinute)
	}
	if directive.FetchWorkers != 7 {
		t.Fatalf("fetch workers = %d, want 7", directive.FetchWorkers)
	}
	if !directive.PrioritizeAutomaticDiscovery {
		t.Fatal("automatic discovery priority = false, want true")
	}
}

func TestDispatchDirectivesNilHandlerNoOp(t *testing.T) {
	acknowledged := dispatchDirectives(context.Background(), nil, []*crawlrpc.CrawlControlDirective{
		{Kind: crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE},
	})
	if acknowledged != nil {
		t.Fatalf("nil handler acknowledgments = %v, want nil", acknowledged)
	}
}

func TestDispatchDirectivesFansToHandler(t *testing.T) {
	handler := &recordingControlHandler{}
	acknowledged := dispatchDirectives(
		context.Background(),
		handler,
		[]*crawlrpc.CrawlControlDirective{
			{DirectiveId: 7, Kind: crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE},
			{Kind: crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL},
		},
	)
	applied := handler.snapshot()
	if len(applied) != 2 || applied[0].Kind != yagocrawlcontract.CrawlControlPause ||
		applied[1].Kind != yagocrawlcontract.CrawlControlCancel {
		t.Fatalf("dispatched = %+v, want pause then cancel", applied)
	}
	if len(acknowledged) != 1 || acknowledged[0] != 7 {
		t.Fatalf("acknowledged = %v, want [7]", acknowledged)
	}
}

type ratedCall struct {
	provenance []byte
	ppm        uint32
}

type fakeController struct {
	paused    [][]byte
	resumed   [][]byte
	cancelled [][]byte
	rated     []ratedCall
}

type durableController struct {
	fakeController
	succeeded bool
}

func (c *durableController) PauseControl(provenance []byte) bool {
	c.Pause(provenance)

	return c.succeeded
}

func (c *durableController) ResumeControl(provenance []byte) bool {
	c.Resume(provenance)

	return c.succeeded
}

func (c *durableController) CancelControl(provenance []byte) bool {
	c.Cancel(provenance)

	return c.succeeded
}

func (c *durableController) SetRateControl(provenance []byte, pagesPerMinute uint32) bool {
	c.SetRate(provenance, pagesPerMinute)

	return c.succeeded
}

func (c *fakeController) Pause(provenance []byte) {
	c.paused = append(c.paused, provenance)
}

func (c *fakeController) Resume(provenance []byte) {
	c.resumed = append(c.resumed, provenance)
}

func (c *fakeController) Cancel(provenance []byte) {
	c.cancelled = append(c.cancelled, provenance)
}

func (c *fakeController) SetRate(provenance []byte, pagesPerMinute uint32) {
	c.rated = append(c.rated, ratedCall{provenance: provenance, ppm: pagesPerMinute})
}

func (c *fakeController) steers() int {
	return len(c.paused) + len(c.resumed) + len(c.cancelled) + len(c.rated)
}

func TestFrontierControlHandlerPauseResume(t *testing.T) {
	controller := &fakeController{}
	handler := NewFrontierControlHandler(controller)

	handler.Apply(context.Background(), yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlPause,
		RunID: "ab",
	})
	handler.Apply(context.Background(), yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlResume,
		RunID: "cd",
	})

	if len(controller.paused) != 1 || controller.paused[0][0] != 0xab {
		t.Fatalf("paused = %x, want [ab]", controller.paused)
	}
	if len(controller.resumed) != 1 || controller.resumed[0][0] != 0xcd {
		t.Fatalf("resumed = %x, want [cd]", controller.resumed)
	}
}

func TestFrontierControlHandlerCancel(t *testing.T) {
	controller := &fakeController{}
	NewFrontierControlHandler(controller).Apply(
		context.Background(),
		yagocrawlcontract.CrawlControlDirective{
			Kind:  yagocrawlcontract.CrawlControlCancel,
			RunID: "ef",
		},
	)
	if len(controller.cancelled) != 1 || controller.cancelled[0][0] != 0xef {
		t.Fatalf("cancelled = %x, want [ef]", controller.cancelled)
	}
}

func TestFrontierControlHandlerIgnoresMalformedRunID(t *testing.T) {
	controller := &fakeController{}
	NewFrontierControlHandler(controller).Apply(
		context.Background(),
		yagocrawlcontract.CrawlControlDirective{
			Kind:  yagocrawlcontract.CrawlControlPause,
			RunID: "not-hex",
		},
	)
	if len(controller.paused) != 0 {
		t.Fatal("a malformed run token must be ignored, not paused")
	}
}

func TestFrontierControlHandlerSetRate(t *testing.T) {
	controller := &fakeController{}
	NewFrontierControlHandler(controller).Apply(
		context.Background(),
		yagocrawlcontract.CrawlControlDirective{
			Kind:           yagocrawlcontract.CrawlControlSetRate,
			RunID:          "ab",
			PagesPerMinute: 45,
		},
	)
	if len(controller.rated) != 1 || controller.rated[0].provenance[0] != 0xab ||
		controller.rated[0].ppm != 45 {
		t.Fatalf("rated = %+v, want [ab]/45", controller.rated)
	}
}

func TestFrontierControlHandlerConfirmsOnlyDurableApplication(t *testing.T) {
	controller := &durableController{succeeded: true}
	handler := NewFrontierControlHandler(controller)
	for _, directive := range []yagocrawlcontract.CrawlControlDirective{
		{Kind: yagocrawlcontract.CrawlControlPause, RunID: "01"},
		{Kind: yagocrawlcontract.CrawlControlResume, RunID: "02"},
		{Kind: yagocrawlcontract.CrawlControlCancel, RunID: "03"},
		{Kind: yagocrawlcontract.CrawlControlSetRate, RunID: "04", PagesPerMinute: 7},
	} {
		if !handler.ApplyControl(t.Context(), directive) {
			t.Fatalf("durable directive = %+v, want confirmed", directive)
		}
	}
	controller.succeeded = false
	if handler.ApplyControl(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlCancel,
		RunID: "05",
	}) {
		t.Fatal("failed durable cancellation was confirmed")
	}
	if handler.ApplyControl(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlPause,
		RunID: "not-hex",
	}) {
		t.Fatal("malformed directive was confirmed")
	}
	if handler.ApplyControl(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlRestart,
		RunID: "06",
	}) {
		t.Fatal("unsupported frontier directive was confirmed")
	}
}

func TestDispatchRetainsFailedDurableDirective(t *testing.T) {
	handler := NewFrontierControlHandler(&durableController{})
	acknowledged := dispatchDirectives(t.Context(), handler, []*crawlrpc.CrawlControlDirective{{
		DirectiveId: 91,
		Kind:        crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL,
		RunId:       []byte{0xab},
	}})
	if len(acknowledged) != 0 {
		t.Fatalf("failed durable acknowledgments = %v, want empty", acknowledged)
	}
}

func TestFrontierControlHandlerIgnoresUnknownKind(t *testing.T) {
	controller := &fakeController{}
	NewFrontierControlHandler(controller).Apply(
		context.Background(),
		yagocrawlcontract.CrawlControlDirective{
			Kind:  yagocrawlcontract.CrawlControlKind("bogus"),
			RunID: "ab",
		},
	)
	if controller.steers() != 0 {
		t.Fatal("an unrecognised control kind must steer nothing")
	}
}

func TestLoggingControlHandlerApplyDoesNotPanic(t *testing.T) {
	LoggingControlHandler{}.Apply(context.Background(), yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlResume,
		RunID: "beef",
	})
}

func TestGRPCOrderReceiverDispatchesHeartbeatDirectives(t *testing.T) {
	fastHeartbeat(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &recordingControlHandler{}
	client := &fakeStreamer{
		ctx: ctx,
		beatDirectives: []*crawlrpc.CrawlControlDirective{
			{Kind: crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL, RunId: []byte{0x01}},
		},
	}

	NewGRPCOrderReceiver(ctx, client, "worker-ctl", handler)
	deadline := time.After(2 * time.Second)
	for len(handler.snapshot()) == 0 {
		select {
		case <-deadline:
			t.Fatal("no directive dispatched from heartbeat")
		case <-time.After(time.Millisecond):
		}
	}
	if got := handler.snapshot()[0]; got.Kind != yagocrawlcontract.CrawlControlCancel ||
		got.RunID != "01" {
		t.Fatalf("dispatched directive = %+v, want cancel/01", got)
	}
}
