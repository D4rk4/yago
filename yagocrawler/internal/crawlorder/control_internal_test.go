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
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE:    yagocrawlcontract.CrawlControlPause,
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESUME:   yagocrawlcontract.CrawlControlResume,
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL:   yagocrawlcontract.CrawlControlCancel,
		crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_RATE: yagocrawlcontract.CrawlControlSetRate,
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
		Kind:           crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_RATE,
		RunId:          []byte{0xab, 0xcd},
		PagesPerMinute: 30,
	})
	if directive.Kind != yagocrawlcontract.CrawlControlSetRate {
		t.Fatalf("kind = %q", directive.Kind)
	}
	if directive.RunID != "abcd" {
		t.Fatalf("run id = %q, want abcd", directive.RunID)
	}
	if directive.PagesPerMinute != 30 {
		t.Fatalf("ppm = %d, want 30", directive.PagesPerMinute)
	}
}

func TestDispatchDirectivesNilHandlerNoOp(t *testing.T) {
	dispatchDirectives(context.Background(), nil, []*crawlrpc.CrawlControlDirective{
		{Kind: crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE},
	})
}

func TestDispatchDirectivesFansToHandler(t *testing.T) {
	handler := &recordingControlHandler{}
	dispatchDirectives(context.Background(), handler, []*crawlrpc.CrawlControlDirective{
		{Kind: crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE},
		{Kind: crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL},
	})
	applied := handler.snapshot()
	if len(applied) != 2 || applied[0].Kind != yagocrawlcontract.CrawlControlPause ||
		applied[1].Kind != yagocrawlcontract.CrawlControlCancel {
		t.Fatalf("dispatched = %+v, want pause then cancel", applied)
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
