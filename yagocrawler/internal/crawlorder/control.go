package crawlorder

import (
	"context"
	"encoding/hex"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

// ControlHandler applies a control directive the node pushed to this worker over a
// heartbeat. The receiver decodes each directive and dispatches it here; the
// concrete behaviours (pause/resume, cancel, rate) live behind this seam.
type ControlHandler interface {
	Apply(ctx context.Context, directive yagocrawlcontract.CrawlControlDirective)
}

// CrawlController is the worker's run-steering surface a control handler drives.
// The frontier satisfies it, keying runs by their provenance token.
type CrawlController interface {
	Pause(provenance []byte)
	Resume(provenance []byte)
	Cancel(provenance []byte)
	SetRate(provenance []byte, pagesPerMinute uint32)
}

// FrontierControlHandler applies control directives to the worker's crawl
// controller, translating a directive's hex run token back to the raw provenance
// the controller keys runs by. A malformed token is logged and ignored.
type FrontierControlHandler struct {
	controller CrawlController
}

// NewFrontierControlHandler binds a control handler to a crawl controller.
func NewFrontierControlHandler(controller CrawlController) FrontierControlHandler {
	return FrontierControlHandler{controller: controller}
}

// Apply dispatches a directive to the controller.
func (h FrontierControlHandler) Apply(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) {
	provenance, err := hex.DecodeString(directive.RunID)
	if err != nil {
		slog.WarnContext(ctx, "crawl control directive has a malformed run token",
			slog.String("run", directive.RunID))

		return
	}

	switch directive.Kind {
	case yagocrawlcontract.CrawlControlPause:
		h.controller.Pause(provenance)
	case yagocrawlcontract.CrawlControlResume:
		h.controller.Resume(provenance)
	case yagocrawlcontract.CrawlControlCancel:
		h.controller.Cancel(provenance)
	case yagocrawlcontract.CrawlControlSetRate:
		h.controller.SetRate(provenance, directive.PagesPerMinute)
	}
}

// LoggingControlHandler records each received directive to the worker log. It is
// the receiver's default handler: control delivery is observable from the moment
// the channel exists, and the run-steering behaviours are layered on top.
type LoggingControlHandler struct{}

// Apply logs the directive without acting on it.
func (LoggingControlHandler) Apply(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) {
	slog.InfoContext(ctx, "crawl control directive received",
		slog.String("kind", string(directive.Kind)),
		slog.String("run", directive.RunID),
		slog.Uint64("pagesPerMinute", uint64(directive.PagesPerMinute)),
	)
}

func dispatchDirectives(
	ctx context.Context,
	handler ControlHandler,
	directives []*crawlrpc.CrawlControlDirective,
) {
	if handler == nil {
		return
	}
	for _, directive := range directives {
		handler.Apply(ctx, directiveFromProto(directive))
	}
}

func directiveFromProto(
	directive *crawlrpc.CrawlControlDirective,
) yagocrawlcontract.CrawlControlDirective {
	return yagocrawlcontract.CrawlControlDirective{
		Kind:           controlKindFromProto(directive.GetKind()),
		RunID:          hex.EncodeToString(directive.GetRunId()),
		PagesPerMinute: directive.GetPagesPerMinute(),
	}
}

func controlKindFromProto(kind crawlrpc.CrawlControlKind) yagocrawlcontract.CrawlControlKind {
	switch kind {
	case crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE:
		return yagocrawlcontract.CrawlControlPause
	case crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESUME:
		return yagocrawlcontract.CrawlControlResume
	case crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL:
		return yagocrawlcontract.CrawlControlCancel
	case crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_RATE:
		return yagocrawlcontract.CrawlControlSetRate
	case crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESTART:
		return yagocrawlcontract.CrawlControlRestart
	default:
		return ""
	}
}
