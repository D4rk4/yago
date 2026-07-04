package crawlbroker

import (
	"encoding/hex"
	"sync"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

// ControlRegistry queues control directives per worker until the worker's next
// heartbeat drains them. It is the node's side of the crawl control plane: the
// admin surface enqueues a directive for the worker running a target run, and the
// heartbeat handler delivers it.
type ControlRegistry struct {
	mu      sync.Mutex
	pending map[string][]yagocrawlcontract.CrawlControlDirective
}

func newControlRegistry() *ControlRegistry {
	return &ControlRegistry{
		pending: make(map[string][]yagocrawlcontract.CrawlControlDirective),
	}
}

// Enqueue queues a directive for a worker; it is delivered on the worker's next
// heartbeat. A blank worker id is ignored, since there is no heartbeat to carry it.
func (r *ControlRegistry) Enqueue(
	workerID string,
	directive yagocrawlcontract.CrawlControlDirective,
) {
	if workerID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.pending[workerID] = append(r.pending[workerID], directive)
}

// drain returns and clears the directives queued for a worker.
func (r *ControlRegistry) drain(workerID string) []yagocrawlcontract.CrawlControlDirective {
	r.mu.Lock()
	defer r.mu.Unlock()

	directives := r.pending[workerID]
	delete(r.pending, workerID)

	return directives
}

func directivesToProto(
	directives []yagocrawlcontract.CrawlControlDirective,
) []*crawlrpc.CrawlControlDirective {
	if len(directives) == 0 {
		return nil
	}

	out := make([]*crawlrpc.CrawlControlDirective, 0, len(directives))
	for _, directive := range directives {
		out = append(out, directiveToProto(directive))
	}

	return out
}

func directiveToProto(
	directive yagocrawlcontract.CrawlControlDirective,
) *crawlrpc.CrawlControlDirective {
	// RunID is a hex provenance token; a malformed id degrades to an empty target,
	// which the worker reads as "the whole worker" rather than a specific run.
	runID, err := hex.DecodeString(directive.RunID)
	if err != nil {
		runID = nil
	}

	return &crawlrpc.CrawlControlDirective{
		Kind:           controlKindToProto(directive.Kind),
		RunId:          runID,
		PagesPerMinute: directive.PagesPerMinute,
	}
}

func controlKindToProto(kind yagocrawlcontract.CrawlControlKind) crawlrpc.CrawlControlKind {
	switch kind {
	case yagocrawlcontract.CrawlControlPause:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_PAUSE
	case yagocrawlcontract.CrawlControlResume:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESUME
	case yagocrawlcontract.CrawlControlCancel:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_CANCEL
	case yagocrawlcontract.CrawlControlSetRate:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_RATE
	default:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_UNSPECIFIED
	}
}
