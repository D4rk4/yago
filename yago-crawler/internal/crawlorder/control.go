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

type confirmingControlHandler interface {
	ApplyControl(ctx context.Context, directive yagocrawlcontract.CrawlControlDirective) bool
}

// CrawlController is the worker's run-steering surface a control handler drives.
// The frontier satisfies it, keying runs by their provenance token.
type CrawlController interface {
	Pause(provenance []byte)
	Resume(provenance []byte)
	Cancel(provenance []byte)
	SetRate(provenance []byte, pagesPerMinute uint32)
}

type durableCrawlController interface {
	PauseControl(provenance []byte) bool
	ResumeControl(provenance []byte) bool
	CancelControl(provenance []byte) bool
	SetRateControl(provenance []byte, pagesPerMinute uint32) bool
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
	h.ApplyControl(ctx, directive)
}

func (h FrontierControlHandler) ApplyControl(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) bool {
	provenance, err := hex.DecodeString(directive.RunID)
	if err != nil {
		slog.WarnContext(ctx, "crawl control directive has a malformed run token",
			slog.String("run", directive.RunID))

		return false
	}

	durable, hasDurability := h.controller.(durableCrawlController)
	switch directive.Kind {
	case yagocrawlcontract.CrawlControlPause:
		if hasDurability {
			return durable.PauseControl(provenance)
		}
		h.controller.Pause(provenance)
	case yagocrawlcontract.CrawlControlResume:
		if hasDurability {
			return durable.ResumeControl(provenance)
		}
		h.controller.Resume(provenance)
	case yagocrawlcontract.CrawlControlCancel:
		if hasDurability {
			return durable.CancelControl(provenance)
		}
		h.controller.Cancel(provenance)
	case yagocrawlcontract.CrawlControlSetRate:
		if hasDurability {
			return durable.SetRateControl(provenance, directive.PagesPerMinute)
		}
		h.controller.SetRate(provenance, directive.PagesPerMinute)
	default:
		return false
	}

	return true
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
	LoggingControlHandler{}.ApplyControl(ctx, directive)
}

func (LoggingControlHandler) ApplyControl(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) bool {
	slog.InfoContext(ctx, "crawl control directive received",
		slog.String("kind", string(directive.Kind)),
		slog.String("run", directive.RunID),
		slog.Uint64("pagesPerMinute", uint64(directive.PagesPerMinute)),
	)

	return true
}

func dispatchDirectives(
	ctx context.Context,
	handler ControlHandler,
	directives []*crawlrpc.CrawlControlDirective,
) []uint64 {
	if handler == nil {
		return nil
	}
	acknowledged := make([]uint64, 0, len(directives))
	for _, directive := range directives {
		if applyControlDirective(ctx, handler, directiveFromProto(directive)) &&
			directive.GetDirectiveId() != 0 {
			acknowledged = append(acknowledged, directive.GetDirectiveId())
		}
	}

	return acknowledged
}

func applyControlDirective(
	ctx context.Context,
	handler ControlHandler,
	directive yagocrawlcontract.CrawlControlDirective,
) bool {
	if confirming, ok := handler.(confirmingControlHandler); ok {
		return confirming.ApplyControl(ctx, directive)
	}
	handler.Apply(ctx, directive)

	return true
}

func directiveFromProto(
	directive *crawlrpc.CrawlControlDirective,
) yagocrawlcontract.CrawlControlDirective {
	return yagocrawlcontract.CrawlControlDirective{
		DirectiveID:                  directive.GetDirectiveId(),
		Kind:                         controlKindFromProto(directive.GetKind()),
		RunID:                        hex.EncodeToString(directive.GetRunId()),
		PagesPerMinute:               directive.GetPagesPerMinute(),
		FetchWorkers:                 directive.GetFetchWorkers(),
		PrioritizeAutomaticDiscovery: directive.GetPrioritizeAutomaticDiscovery(),
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
	case crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_WORKERS:
		return yagocrawlcontract.CrawlControlSetWorkers
	case crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_AUTOMATIC_DISCOVERY_PRIORITY:
		return yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority
	default:
		return ""
	}
}
