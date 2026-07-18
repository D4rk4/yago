package yagocrawlcontract

// CrawlControlKind is a directive the node pushes to a worker to steer a run it is
// executing.
type CrawlControlKind string

const (
	// CrawlControlPause gates fetching on the targeted run without dropping it.
	CrawlControlPause CrawlControlKind = "pause"
	// CrawlControlResume lifts a pause.
	CrawlControlResume CrawlControlKind = "resume"
	// CrawlControlCancel drains the targeted run and settles its orders.
	CrawlControlCancel CrawlControlKind = "cancel"
	// CrawlControlSetRate caps the run's fetch pace at PagesPerMinute.
	CrawlControlSetRate CrawlControlKind = "set_rate"
	// CrawlControlRestart asks the whole worker to shut down gracefully so its
	// supervisor brings it back up; RunID is ignored since it targets the process.
	CrawlControlRestart                       CrawlControlKind = "restart"
	CrawlControlSetWorkers                    CrawlControlKind = "set_workers"
	CrawlControlSetActiveRuns                 CrawlControlKind = "set_active_runs"
	CrawlControlSetAutomaticDiscoveryPriority CrawlControlKind = "set_automatic_discovery_priority"
)

// CrawlControlDirective steers one run identified by its provenance token (RunID,
// the same hex token the progress report carries), or the whole worker when RunID
// is empty. PagesPerMinute carries the cap for a CrawlControlSetRate directive and
// is ignored otherwise.
type CrawlControlDirective struct {
	DirectiveID                  uint64
	Kind                         CrawlControlKind
	RunID                        string
	PagesPerMinute               uint32
	FetchWorkers                 uint32
	MaximumActiveRuns            uint32
	PrioritizeAutomaticDiscovery bool
}
