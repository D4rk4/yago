package crawlbroker

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type failingTerminalSettlementEntropy struct{}

func (failingTerminalSettlementEntropy) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

type failingTerminalSettlementSecretCodec struct{}

func (failingTerminalSettlementSecretCodec) Encode(value []byte) ([]byte, error) {
	return value, nil
}

func (failingTerminalSettlementSecretCodec) Decode([]byte) ([]byte, error) {
	return nil, errors.New("decode failed")
}

func TestTerminalSettlementTokenBindsEveryExactField(t *testing.T) {
	secret, request := terminalSettlementTokenFixture(t)
	request.ConfirmationToken = terminalSettlementToken(secret, "lease", request)
	if !validTerminalSettlementToken(secret, "lease", request) {
		t.Fatal("exact terminal settlement token is invalid")
	}
	assertTerminalSettlementRequestMutations(t, secret, request)
	assertTerminalSettlementOutcomeMutations(t, secret, request)
	if validTerminalSettlementToken(secret, "other-lease", request) {
		t.Fatal("terminal token accepted another lease")
	}
	request.ConfirmationToken = []byte("short")
	if validTerminalSettlementToken(secret, "lease", request) {
		t.Fatal("short terminal token is valid")
	}
}

func terminalSettlementTokenFixture(t *testing.T) ([]byte, terminalLeaseRequest) {
	t.Helper()
	secret := bytes.Repeat([]byte{7}, 32)
	outcomes, err := yagocrawlcontract.NewCrawlURLOutcomeHistory(
		[]yagocrawlcontract.CrawlURLOutcome{
			{
				Sequence:   1,
				URL:        "https://example.com/",
				Class:      yagocrawlcontract.CrawlURLOutcomeFailed,
				ObservedAt: time.UnixMilli(1_000).UTC(),
				HTTPStatus: 503,
				Reason:     "fetch failed",
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := terminalLeaseRequest{
		Outcome:         leaseSettlementAcknowledged,
		OrderIdentity:   bytes.Repeat([]byte{1}, 32),
		WorkerID:        "worker",
		WorkerSessionID: "session",
		State:           yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{
			Fetched: 1, Indexed: 2, Failed: 3, RobotsDenied: 4, Duplicates: 5,
		},
		Rate:           30,
		RateKnown:      true,
		RecentOutcomes: outcomes,
	}

	return secret, request
}

func assertTerminalSettlementRequestMutations(
	t *testing.T,
	secret []byte,
	request terminalLeaseRequest,
) {
	t.Helper()
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
}

func assertTerminalSettlementOutcomeMutations(
	t *testing.T,
	secret []byte,
	request terminalLeaseRequest,
) {
	t.Helper()
	outcomeMutations := []func(*yagocrawlcontract.CrawlURLOutcome){
		func(outcome *yagocrawlcontract.CrawlURLOutcome) { outcome.Sequence++ },
		func(outcome *yagocrawlcontract.CrawlURLOutcome) { outcome.URL += "other" },
		func(outcome *yagocrawlcontract.CrawlURLOutcome) {
			outcome.Class = yagocrawlcontract.CrawlURLOutcomeSkipped
		},
		func(outcome *yagocrawlcontract.CrawlURLOutcome) {
			outcome.ObservedAt = outcome.ObservedAt.Add(time.Millisecond)
		},
		func(outcome *yagocrawlcontract.CrawlURLOutcome) { outcome.HTTPStatus++ },
		func(outcome *yagocrawlcontract.CrawlURLOutcome) { outcome.Reason += " other" },
	}
	for index, mutate := range outcomeMutations {
		changed := request
		changedOutcomes := request.RecentOutcomes.Chronological()
		mutate(&changedOutcomes[0])
		var err error
		changed.RecentOutcomes, err = yagocrawlcontract.NewCrawlURLOutcomeHistory(
			changedOutcomes,
		)
		if err != nil {
			t.Fatalf("outcome mutation %d: %v", index, err)
		}
		if validTerminalSettlementToken(secret, "lease", changed) {
			t.Fatalf("terminal token outcome mutation %d remained valid", index)
		}
	}
}

func TestTerminalSettlementSecretCreationFailuresCloseQueueConstruction(t *testing.T) {
	restoreEntropy := terminalSettlementEntropy
	t.Cleanup(func() { terminalSettlementEntropy = restoreEntropy })

	t.Run("entropy", func(t *testing.T) {
		assertTerminalSettlementEntropyFailure(t, restoreEntropy)
	})

	t.Run("write", func(t *testing.T) {
		assertTerminalSettlementWriteFailure(t)
	})

	t.Run("read", func(t *testing.T) {
		assertTerminalSettlementReadFailure(t)
	})

	t.Run("length", func(t *testing.T) {
		assertTerminalSettlementLengthFailure(t)
	})
}

func assertTerminalSettlementEntropyFailure(t *testing.T, restoreEntropy io.Reader) {
	t.Helper()
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
}

func assertTerminalSettlementWriteFailure(t *testing.T) {
	t.Helper()
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
}

func assertTerminalSettlementReadFailure(t *testing.T) {
	t.Helper()
	engine := newScriptedEngine()
	persisted := bytes.Repeat([]byte{9}, sha256.Size)
	engine.buckets[terminalSettlementSecretBucket] = map[string][]byte{
		string(terminalSettlementSecretKey): persisted,
	}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("open secret read storage: %v", err)
	}
	secrets, err := vault.Register(
		storage,
		terminalSettlementSecretBucket,
		failingTerminalSettlementSecretCodec{},
	)
	if err != nil {
		t.Fatalf("register secret read storage: %v", err)
	}
	if _, err := loadTerminalSettlementSecret(storage, secrets); err == nil ||
		!strings.Contains(err.Error(), "read terminal settlement secret") {
		t.Fatalf("terminal settlement secret read error = %v", err)
	}
	if !bytes.Equal(
		engine.buckets[terminalSettlementSecretBucket][string(terminalSettlementSecretKey)],
		persisted,
	) {
		t.Fatal("terminal settlement secret changed after a read failure")
	}
}

func assertTerminalSettlementLengthFailure(t *testing.T) {
	t.Helper()
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
}
