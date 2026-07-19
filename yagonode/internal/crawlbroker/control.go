package crawlbroker

import (
	"context"
	"encoding/hex"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type ControlRegistry struct {
	mu                            sync.Mutex
	processRateUpdate             sync.Mutex
	directives                    controlDirectiveLedger
	fetchWorkers                  uint32
	fetchWorkersSet               bool
	processPagesPerSecond         uint32
	processRateSet                bool
	maximumRedirects              uint32
	maximumRedirectsSet           bool
	maximumActiveRuns             uint32
	maximumActiveRunsSet          bool
	prioritizeAutomaticDiscovery  bool
	automaticDiscoveryPrioritySet bool
	// workers counts the live StreamOrders connections per worker id so a
	// broadcast (RestartWorkers) reaches exactly the crawlers attached now.
	workers                map[string]int
	runWorkers             map[string]string
	activeFetches          map[string]crawlerActiveFetches
	storageStates          map[string]crawlerStorageState
	initialized            map[string]bool
	storagePressurePolicy  yagocrawlcontract.StoragePressurePolicy
	runtimePolicy          yagocrawlcontract.CrawlerRuntimePolicy
	now                    func() time.Time
	setFleetPagesPerSecond func(uint32) error
}

func newControlRegistry(defaults ...crawlerControlDefaults) *ControlRegistry {
	return newControlRegistryWithLedger(newMemoryControlDirectiveLedger(), defaults...)
}

func newControlRegistryWithLedger(
	directives controlDirectiveLedger,
	defaults ...crawlerControlDefaults,
) *ControlRegistry {
	registry := &ControlRegistry{
		directives:    directives,
		workers:       make(map[string]int),
		runWorkers:    make(map[string]string),
		activeFetches: make(map[string]crawlerActiveFetches),
		storageStates: make(map[string]crawlerStorageState),
		initialized:   make(map[string]bool),
		now:           time.Now,
		runtimePolicy: yagocrawlcontract.DefaultCrawlerRuntimePolicy(),
	}
	if len(defaults) > 0 {
		registry.fetchWorkers = defaults[0].fetchWorkers
		registry.fetchWorkersSet = defaults[0].fetchWorkers > 0
		registry.processPagesPerSecond = defaults[0].processPagesPerSecond
		registry.processRateSet = defaults[0].processRateSet
		registry.maximumRedirects = defaults[0].maximumRedirects
		registry.maximumRedirectsSet = defaults[0].maximumRedirectsSet
		registry.maximumActiveRuns = defaults[0].maximumActiveRuns
		registry.maximumActiveRunsSet = defaults[0].maximumActiveRuns > 0
		registry.prioritizeAutomaticDiscovery = defaults[0].prioritizeAutomaticDiscovery
		registry.automaticDiscoveryPrioritySet = true
		registry.storagePressurePolicy = defaults[0].storagePressurePolicy
		if defaults[0].runtimePolicy.Validate() == nil {
			registry.runtimePolicy = defaults[0].runtimePolicy
		}
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
		delete(r.storageStates, workerID)
		if !r.initialized[workerID] {
			r.initialized[workerID] = r.ensureInitialLocked(context.Background(), workerID) == nil
		}
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
	signalled := 0
	for workerID := range r.workers {
		if r.enqueueLocked(workerID, directive) {
			signalled++
		} else {
			r.initialized[workerID] = false
		}
	}

	return signalled
}

func (r *ControlRegistry) unregister(workerID string) bool {
	if workerID == "" {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.workers[workerID] <= 1 {
		delete(r.workers, workerID)
		delete(r.activeFetches, workerID)
		delete(r.storageStates, workerID)
		delete(r.initialized, workerID)

		return true
	}
	r.workers[workerID]--

	return false
}

func (r *ControlRegistry) RestartWorkers() int {
	restart := yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlRestart}

	r.mu.Lock()
	defer r.mu.Unlock()

	signalled := 0
	for workerID := range r.workers {
		if r.enqueueLocked(workerID, restart) {
			signalled++
		}
	}

	return signalled
}

// Enqueue queues a directive for its current run worker or the supplied worker.
// A blank worker id is ignored, since there is no heartbeat to carry it.
func (r *ControlRegistry) Enqueue(
	workerID string,
	directive yagocrawlcontract.CrawlControlDirective,
) bool {
	if workerID == "" {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if !validControlDirective(directive) {
		return false
	}
	targetWorkerID := workerID
	if assignedWorkerID := r.runWorkers[directive.RunID]; assignedWorkerID != "" {
		targetWorkerID = assignedWorkerID
	}
	if r.workers[targetWorkerID] == 0 && directive.RunID == "" {
		return false
	}

	return r.enqueueLocked(targetWorkerID, directive)
}

func (r *ControlRegistry) enqueueLocked(
	workerID string,
	directive yagocrawlcontract.CrawlControlDirective,
) bool {
	_, err := r.directives.Enqueue(context.Background(), workerID, directive)

	return err == nil
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
		DirectiveId:                  directive.DirectiveID,
		Kind:                         controlKindToProto(directive.Kind),
		RunId:                        runID,
		PagesPerMinute:               directive.PagesPerMinute,
		FetchWorkers:                 directive.FetchWorkers,
		MaximumActiveRuns:            directive.MaximumActiveRuns,
		ProcessPagesPerSecond:        directive.ProcessPagesPerSecond,
		MaximumRedirects:             directive.MaximumRedirects,
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
	case yagocrawlcontract.CrawlControlSetActiveRuns:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_ACTIVE_RUNS
	case yagocrawlcontract.CrawlControlSetProcessRate:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_PROCESS_RATE
	case yagocrawlcontract.CrawlControlSetMaximumRedirects:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_MAXIMUM_REDIRECTS
	case yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_AUTOMATIC_DISCOVERY_PRIORITY
	default:
		return crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_UNSPECIFIED
	}
}
