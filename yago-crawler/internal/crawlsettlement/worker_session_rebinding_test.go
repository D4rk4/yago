package crawlsettlement

import "testing"

func TestTerminalSettlementSessionRebindingPreservesEveryOtherDefinitionField(t *testing.T) {
	original := validTestSettlement()
	rebound := Clone(original)
	rebound.WorkerSessionID = "replacement-session"
	if !SameDefinitionExceptWorkerSession(original, rebound) {
		t.Fatal("worker session was treated as an immutable settlement field")
	}
	rebound.WorkerID = "replacement-worker"
	if SameDefinitionExceptWorkerSession(original, rebound) {
		t.Fatal("worker identity change was accepted as a session-only rebind")
	}
}
