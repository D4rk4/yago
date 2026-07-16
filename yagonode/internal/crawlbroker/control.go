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
	mu                            sync.Mutex
	pending                       map[string][]yagocrawlcontract.CrawlControlDirective
	fetchWorkers                  uint32
	fetchWorkersSet               bool
	prioritizeAutomaticDiscovery  bool
	automaticDiscoveryPrioritySet bool
	// workers counts the live StreamOrders connections per worker id so a
	// broadcast (RestartWorkers) reaches exactly the crawlers attached now.
	workers       map[string]int
	activeFetches map[string]uint32
}

func newControlRegistry(defaults ...crawlerControlDefaults) *ControlRegistry {
	registry := &ControlRegistry{
		pending:       make(map[string][]yagocrawlcontract.CrawlControlDirective),
		workers:       make(map[string]int),
		activeFetches: make(map[string]uint32),
	}
	if len(defaults) > 0 {
		registry.fetchWorkers = defaults[0].fetchWorkers
		registry.fetchWorkersSet = defaults[0].fetchWorkers > 0
		registry.prioritizeAutomaticDiscovery = defaults[0].prioritizeAutomaticDiscovery
		registry.automaticDiscoveryPrioritySet = true
	}

	return registry
}

// register marks a worker's order stream as connected; a blank id is ignored.
func (r *ControlRegistry) register(workerID string) {
	if workerID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.workers[workerID]++
	if r.workers[workerID] == 1 {
		delete(r.activeFetches, workerID)
		r.pending[workerID] = append(r.pending[workerID], r.initialDirectivesLocked()...)
	}
}

func (r *ControlRegistry) SetFetchWorkers(fetchWorkers int) int {
	if fetchWorkers < 1 || fetchWorkers > yagocrawlcontract.MaximumFetchWorkerConcurrency {
		return 0
	}

	directive := yagocrawlcontract.CrawlControlDirective{
		Kind:         yagocrawlcontract.CrawlControlSetWorkers,
		FetchWorkers: uint32(fetchWorkers),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fetchWorkers = uint32(fetchWorkers)
	r.fetchWorkersSet = true
	for workerID := range r.workers {
		r.pending[workerID] = append(r.pending[workerID], directive)
	}

	return len(r.workers)
}

func (r *ControlRegistry) unregister(workerID string) bool {
	if workerID == "" {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.workers[workerID] <= 1 {
		delete(r.workers, workerID)
		delete(r.pending, workerID)
		delete(r.activeFetches, workerID)

		return true
	}
	r.workers[workerID]--

	return false
}

// RestartWorkers queues a restart directive for every connected worker and
// returns how many were signalled. Each directive is one-shot: a worker drains
// it on its next heartbeat, shuts down, and reconnects without it, so the
// broadcast does not loop.
func (r *ControlRegistry) RestartWorkers() int {
	restart := yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlRestart}

	r.mu.Lock()
	defer r.mu.Unlock()

	for workerID := range r.workers {
		r.pending[workerID] = append(r.pending[workerID], restart)
	}

	return len(r.workers)
}

// Enqueue queues a directive for a worker; it is delivered on the worker's next
// heartbeat. A blank worker id is ignored, since there is no heartbeat to carry it.
func (r *ControlRegistry) Enqueue(
	workerID string,
	directive yagocrawlcontract.CrawlControlDirective,
) bool {
	if workerID == "" {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.workers[workerID] == 0 {
		return false
	}

	r.pending[workerID] = append(r.pending[workerID], directive)

	return true
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
		Kind:                         controlKindToProto(directive.Kind),
		RunId:                        runID,
		PagesPerMinute:               directive.PagesPerMinute,
		FetchWorkers:                 directive.FetchWorkers,
		PrioritizeAutomaticDiscovery: directive.PrioritizeAutomaticDiscovery,
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
	case yagocrawlcontract.CrawlControlRestart:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_RESTART
	case yagocrawlcontract.CrawlControlSetWorkers:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_WORKERS
	case yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_AUTOMATIC_DISCOVERY_PRIORITY
	default:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_UNSPECIFIED
	}
}
