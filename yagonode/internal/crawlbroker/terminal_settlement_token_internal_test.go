package crawlbroker

import (
	"bytes"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type failingTerminalSettlementEntropy struct{}

func (failingTerminalSettlementEntropy) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestTerminalSettlementTokenBindsEveryExactField(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, 32)
	request := terminalLeaseRequest{
		Outcome:         leaseSettlementAcknowledged,
		OrderIdentity:   bytes.Repeat([]byte{1}, 32),
		WorkerID:        "worker",
		WorkerSessionID: "session",
		State:           yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{
			Fetched: 1, Indexed: 2, Failed: 3, RobotsDenied: 4, Duplicates: 5,
		},
		Rate:      30,
		RateKnown: true,
	}
	request.ConfirmationToken = terminalSettlementToken(secret, "lease", request)
	if !validTerminalSettlementToken(secret, "lease", request) {
		t.Fatal("exact terminal settlement token is invalid")
	}
	mutations := []func(*terminalLeaseRequest){
		func(value *terminalLeaseRequest) { value.Outcome = leaseSettlementRequeued },
		func(value *terminalLeaseRequest) { value.OrderIdentity[0]++ },
		func(value *terminalLeaseRequest) { value.WorkerID += "-other" },
		func(value *terminalLeaseRequest) { value.WorkerSessionID += "-other" },
		func(value *terminalLeaseRequest) { value.State = yagocrawlcontract.CrawlRunCancelled },
		func(value *terminalLeaseRequest) { value.Tally.Fetched++ },
		func(value *terminalLeaseRequest) { value.Tally.Indexed++ },
		func(value *terminalLeaseRequest) { value.Tally.Failed++ },
		func(value *terminalLeaseRequest) { value.Tally.RobotsDenied++ },
		func(value *terminalLeaseRequest) { value.Tally.Duplicates++ },
		func(value *terminalLeaseRequest) { value.Tally.Pending++ },
		func(value *terminalLeaseRequest) { value.Rate++ },
		func(value *terminalLeaseRequest) { value.RateKnown = false },
	}
	for index, mutate := range mutations {
		changed := request
		changed.OrderIdentity = append([]byte(nil), request.OrderIdentity...)
		mutate(&changed)
		if validTerminalSettlementToken(secret, "lease", changed) {
			t.Fatalf("terminal token mutation %d remained valid", index)
		}
	}
	if validTerminalSettlementToken(secret, "other-lease", request) {
		t.Fatal("terminal token accepted another lease")
	}
	request.ConfirmationToken = []byte("short")
	if validTerminalSettlementToken(secret, "lease", request) {
		t.Fatal("short terminal token is valid")
	}
}

func TestTerminalSettlementSecretCreationFailuresCloseQueueConstruction(t *testing.T) {
	restoreEntropy := terminalSettlementEntropy
	t.Cleanup(func() { terminalSettlementEntropy = restoreEntropy })

	t.Run("entropy", func(t *testing.T) {
		terminalSettlementEntropy = failingTerminalSettlementEntropy{}
		engine := newScriptedEngine()
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatalf("open entropy storage: %v", err)
		}
		if _, err := newDurableOrderQueue(storage, DefaultLeaseTTL); err == nil {
			t.Fatal("terminal settlement entropy failure was hidden")
		}
		terminalSettlementEntropy = restoreEntropy
	})

	t.Run("write", func(t *testing.T) {
		engine := newScriptedEngine()
		engine.putKeyErrors[terminalSettlementSecretBucket] = map[string]error{
			string(terminalSettlementSecretKey): errors.New("write failed"),
		}
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatalf("open secret write storage: %v", err)
		}
		if _, err := newDurableOrderQueue(storage, DefaultLeaseTTL); err == nil {
			t.Fatal("terminal settlement secret write failure was hidden")
		}
	})

	t.Run("length", func(t *testing.T) {
		engine := newScriptedEngine()
		engine.buckets[terminalSettlementSecretBucket] = map[string][]byte{
			string(terminalSettlementSecretKey): {1},
		}
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatalf("open corrupt secret storage: %v", err)
		}
		if _, err := newDurableOrderQueue(storage, DefaultLeaseTTL); err == nil {
			t.Fatal("corrupt terminal settlement secret was accepted")
		}
	})
}
