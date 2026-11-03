package crawlbroker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestTerminalSettlementProgressKeepsWorkerSessionIdentity(t *testing.T) {
	workerSessionID := "session-current"
	settlement := terminalSettlementRecord(terminalLeaseRequest{
		Outcome:         leaseSettlementAcknowledged,
		OrderIdentity:   make([]byte, 32),
		WorkerID:        "worker-a",
		WorkerSessionID: workerSessionID,
		State:           yagocrawlcontract.CrawlRunFinished,
		Tally:           yagocrawlcontract.CrawlRunTally{Fetched: 4, Indexed: 3},
	})
	if settlement.WorkerSessionID != workerSessionID ||
		settlement.Progress.ProcessIdentifier != workerSessionID {
		t.Fatalf("terminal settlement worker session = %q, progress process identity = %q",
			settlement.WorkerSessionID,
			settlement.Progress.ProcessIdentifier,
		)
	}
}
