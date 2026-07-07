package crawlorder

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

// RestartControlHandler intercepts the node's restart directive and hands it to a
// process-restart trigger, delegating every other directive to the run-steering
// handler underneath. Restart is a whole-worker action, not run steering, so it
// sits in front of the frontier handler rather than inside it.
type RestartControlHandler struct {
	restart func()
	next    ControlHandler
}

// NewRestartControlHandler wraps a run-steering handler so restart directives
// trigger a graceful worker shutdown instead of reaching the frontier.
func NewRestartControlHandler(restart func(), next ControlHandler) RestartControlHandler {
	return RestartControlHandler{restart: restart, next: next}
}

// Apply fires the restart trigger for a restart directive and otherwise defers to
// the wrapped handler.
func (h RestartControlHandler) Apply(
	ctx context.Context,
	directive yagocrawlcontract.CrawlControlDirective,
) {
	if directive.Kind == yagocrawlcontract.CrawlControlRestart {
		slog.InfoContext(ctx, "crawler restart requested by node")
		if h.restart != nil {
			h.restart()
		}

		return
	}
	if h.next != nil {
		h.next.Apply(ctx, directive)
	}
}
