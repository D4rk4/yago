package crawlbroker

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func persistentControlLedgerFixture(
	t *testing.T,
) (*persistentControlDirectiveLedger, *scriptedEngine) {
	t.Helper()
	engine := newScriptedEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	ledger, err := newPersistentControlDirectiveLedger(storage)
	if err != nil {
		t.Fatalf("open control ledger: %v", err)
	}

	return ledger, engine
}

func TestControlDirectiveRecordCodecRejectsCorruption(t *testing.T) {
	if _, err := (controlDirectiveRecordCodec{}).Decode([]byte("{")); err == nil {
		t.Fatal("expected corrupt directive record error")
	}
}

func TestPersistentControlLedgerRegistrationFailures(t *testing.T) {
	for _, bucket := range []vault.Name{controlDirectiveBucket, controlDirectiveState} {
		engine := newScriptedEngine()
		engine.provisionErrors[bucket] = errors.New("provision failed")
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatalf("open storage: %v", err)
		}
		if _, err := newPersistentControlDirectiveLedger(storage); err == nil {
			t.Fatalf("expected %s registration failure", bucket)
		}
	}
}

func TestPersistentControlLedgerEnqueueFailures(t *testing.T) {
	t.Run("sequence read", func(t *testing.T) {
		ledger, engine := persistentControlLedgerFixture(t)
		engine.buckets[controlDirectiveState][string(controlDirectiveNextKey)] = []byte{1}
		if _, err := ledger.Enqueue(
			t.Context(),
			"worker",
			yagocrawlcontract.CrawlControlDirective{},
		); err == nil {
			t.Fatal("expected sequence read failure")
		}
	})
	t.Run("directive write", func(t *testing.T) {
		ledger, engine := persistentControlLedgerFixture(t)
		engine.putErrors[controlDirectiveBucket] = errors.New("put failed")
		if _, err := ledger.Enqueue(
			t.Context(),
			"worker",
			yagocrawlcontract.CrawlControlDirective{},
		); err == nil {
			t.Fatal("expected directive write failure")
		}
	})
	t.Run("sequence write", func(t *testing.T) {
		ledger, engine := persistentControlLedgerFixture(t)
		engine.putErrors[controlDirectiveState] = errors.New("put failed")
		if _, err := ledger.Enqueue(
			t.Context(),
			"worker",
			yagocrawlcontract.CrawlControlDirective{},
		); err == nil {
			t.Fatal("expected sequence write failure")
		}
	})
}

func TestPersistentControlLedgerExchangeFailures(t *testing.T) {
	t.Run("scan", func(t *testing.T) {
		ledger, engine := persistentControlLedgerFixture(t)
		engine.scanErrors[controlDirectiveBucket] = errors.New("scan failed")
		if _, err := ledger.Exchange(t.Context(), "worker", nil); err == nil {
			t.Fatal("expected scan failure")
		}
	})
	t.Run("acknowledged read", func(t *testing.T) {
		ledger, engine := persistentControlLedgerFixture(t)
		engine.buckets[controlDirectiveBucket][string(orderKey(1))] = []byte("{")
		if _, err := ledger.Exchange(t.Context(), "worker", []uint64{1}); err == nil {
			t.Fatal("expected acknowledged read failure")
		}
	})
	t.Run("acknowledged delete", func(t *testing.T) {
		ledger, engine := persistentControlLedgerFixture(t)
		directive, err := ledger.Enqueue(
			t.Context(),
			"worker",
			yagocrawlcontract.CrawlControlDirective{},
		)
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		engine.deleteErrors[controlDirectiveBucket] = errors.New("delete failed")
		if _, err := ledger.Exchange(
			t.Context(),
			"worker",
			[]uint64{directive.DirectiveID},
		); err == nil {
			t.Fatal("expected acknowledged delete failure")
		}
	})
	t.Run("post-ack scan", func(t *testing.T) {
		ledger, engine := persistentControlLedgerFixture(t)
		engine.scanErrors[controlDirectiveBucket] = errors.New("scan failed")
		if _, err := ledger.Exchange(t.Context(), "worker", []uint64{999}); err == nil {
			t.Fatal("expected post-ack scan failure")
		}
	})
}

func TestPersistentControlLedgerRunReconciliationFailures(t *testing.T) {
	t.Run("scan", func(t *testing.T) {
		ledger, engine := persistentControlLedgerFixture(t)
		engine.scanErrors[controlDirectiveBucket] = errors.New("scan failed")
		if err := ledger.ReconcileRun(t.Context(), "worker", "ab", false); err == nil {
			t.Fatal("expected run scan failure")
		}
	})
	t.Run("move", func(t *testing.T) {
		ledger, engine := persistentControlLedgerFixture(t)
		if _, err := ledger.Enqueue(
			t.Context(),
			"worker",
			yagocrawlcontract.CrawlControlDirective{RunID: "ab"},
		); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		engine.putErrors[controlDirectiveBucket] = errors.New("put failed")
		if err := ledger.ReconcileRun(t.Context(), "other", "ab", false); err == nil {
			t.Fatal("expected run move failure")
		}
	})
	t.Run("delete", func(t *testing.T) {
		ledger, engine := persistentControlLedgerFixture(t)
		if _, err := ledger.Enqueue(
			t.Context(),
			"worker",
			yagocrawlcontract.CrawlControlDirective{RunID: "ab"},
		); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		engine.deleteErrors[controlDirectiveBucket] = errors.New("delete failed")
		if err := ledger.ReconcileRun(t.Context(), "worker", "ab", true); err == nil {
			t.Fatal("expected terminal run delete failure")
		}
	})
	t.Run("transaction replay scan", func(t *testing.T) {
		ledger, engine := persistentControlLedgerFixture(t)
		if _, err := ledger.Enqueue(
			t.Context(),
			"worker",
			yagocrawlcontract.CrawlControlDirective{RunID: "ab"},
		); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		engine.replayNext = true
		engine.betweenReplay = func() {
			engine.scanErrors[controlDirectiveBucket] = errors.New("scan failed")
		}
		if err := ledger.ReconcileRun(t.Context(), "other", "ab", false); err == nil {
			t.Fatal("expected replayed run scan failure")
		}
	})
}
