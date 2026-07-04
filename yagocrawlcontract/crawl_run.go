package yagocrawlcontract

// CrawlRunState is the lifecycle stage a worker reports for a run. A run is
// Running while its frontier still has work, Finished when it drains, and
// Cancelled when the operator stops it.
type CrawlRunState string

const (
	CrawlRunRunning   CrawlRunState = "running"
	CrawlRunFinished  CrawlRunState = "finished"
	CrawlRunCancelled CrawlRunState = "cancelled"
)

// CrawlRunTally is the cumulative outcome of a run's pages. It is reported as an
// absolute snapshot, not a delta, so a lost report is corrected by the next one.
type CrawlRunTally struct {
	Fetched      uint64
	Indexed      uint64
	Failed       uint64
	RobotsDenied uint64
	Duplicates   uint64
	Pending      uint64
}

// CrawlRunProgress is a worker's report about one run, keyed by the hex-encoded
// order provenance token so the node and the worker agree on the run identity
// without the node owning the worker's in-memory frontier.
type CrawlRunProgress struct {
	RunID         string
	WorkerID      string
	ProfileHandle string
	ProfileName   string
	State         CrawlRunState
	Tally         CrawlRunTally
}
