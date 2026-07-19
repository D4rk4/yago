package remotecrawl

import (
	"context"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func remoteCrawlFaultLease(
	t *testing.T,
) (*Broker, *vault.Vault, *remoteCrawlFaultEngine, queueRecord) {
	t.Helper()
	broker, storage, engine := openRemoteCrawlFaultBroker(t)
	stageURL(t, broker, testURLA)
	if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err != nil {
		t.Fatal(err)
	}
	var record queueRecord
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		var found bool
		var err error
		record, found, err = broker.orders.Get(tx, sequenceKey(0))
		if err != nil {
			return fmt.Errorf("read leased record: %w", err)
		}
		if !found {
			t.Fatal("leased record missing")
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return broker, storage, engine, record
}

func TestRemoteCrawlStageReportsEveryDurableWriteFailure(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*remoteCrawlFaultEngine)
	}{
		{
			name: "queue length",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putRaw(
					vault.Name("__lengths__"),
					vault.Key(remoteCrawlOrderBucket),
					[]byte{1},
				)
			},
		},
		{
			name: "sequence read",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putRaw(remoteCrawlSequenceBucket, nextSequenceKey, []byte{1})
			},
		},
		{
			name: "order write",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putFailure = remoteCrawlOrderBucket
			},
		},
		{
			name: "pending write",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putFailure = remoteCrawlPendingBucket
			},
		},
		{
			name: "URL sequence write",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putFailure = remoteCrawlURLSequenceBucket
			},
		},
		{
			name: "sequence write",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putFailure = remoteCrawlSequenceBucket
			},
		},
		{
			name: "transaction",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.updateFailure = true
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broker, _, engine := openRemoteCrawlFaultBroker(t)
			test.prepare(engine)
			if err := broker.StageOrder(t.Context(), remoteCrawlOrder(testURLA)); err == nil {
				t.Fatal("stage storage failure ignored")
			}
		})
	}
}

func remoteCrawlOrder(rawURL string) yagocrawlcontract.CrawlOrder {
	return yagocrawlcontract.CrawlOrder{
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL: rawURL, Mode: yagocrawlcontract.CrawlRequestModeURL,
		}},
	}
}

func TestRemoteCrawlCandidateReadsRejectCorruptIndexes(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*remoteCrawlFaultEngine)
	}{
		{
			name: "lease count",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putRaw(remoteCrawlLeaseCountBucket, vault.Key(testPeerA.String()), []byte{1})
			},
		},
		{
			name: "pending index",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putRaw(remoteCrawlPendingBucket, sequenceKey(0), []byte{1})
			},
		},
		{
			name: "missing order",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putRaw(
					remoteCrawlPendingBucket,
					sequenceKey(0),
					remoteCrawlRawJSON(t, pendingRecord{Sequence: 0}),
				)
			},
		},
		{
			name: "corrupt order",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putRaw(
					remoteCrawlPendingBucket,
					sequenceKey(0),
					remoteCrawlRawJSON(t, pendingRecord{Sequence: 0}),
				)
				engine.putRaw(remoteCrawlOrderBucket, sequenceKey(0), []byte{1})
			},
		},
		{
			name: "view",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.viewFailure = true
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broker, _, engine := openRemoteCrawlFaultBroker(t)
			test.prepare(engine)
			if _, _, err := broker.leaseCandidates(t.Context(), testPeerA); err == nil {
				t.Fatal("candidate read failure ignored")
			}
		})
	}
}

func TestRemoteCrawlLeaseSurfacesSelectionAndClaimFailures(t *testing.T) {
	t.Run("selection", func(t *testing.T) {
		broker, _, engine := openRemoteCrawlFaultBroker(t)
		stageURL(t, broker, testURLA)
		broker.policy.resolver = func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		engine.deleteFailure = remoteCrawlOrderBucket
		if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err == nil {
			t.Fatal("selection failure ignored")
		}
	})
	t.Run("claim", func(t *testing.T) {
		broker, _, engine := openRemoteCrawlFaultBroker(t)
		stageURL(t, broker, testURLA)
		engine.putFailure = remoteCrawlOrderBucket
		if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err == nil {
			t.Fatal("claim failure ignored")
		}
	})
	t.Run("preparation candidates", func(t *testing.T) {
		broker, _, engine := openRemoteCrawlFaultBroker(t)
		engine.putRaw(remoteCrawlLeaseCountBucket, vault.Key(testPeerA.String()), []byte{1})
		if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err == nil {
			t.Fatal("candidate preparation failure ignored")
		}
	})
}

func TestRemoteCrawlClaimReportsEveryDurableFailure(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*remoteCrawlFaultEngine)
	}{
		{
			name: "lease count read",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putRaw(remoteCrawlLeaseCountBucket, vault.Key(testPeerA.String()), []byte{1})
			},
		},
		{
			name: "order read",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putRaw(remoteCrawlOrderBucket, sequenceKey(0), []byte{1})
			},
		},
		{
			name: "order write",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putFailure = remoteCrawlOrderBucket
			},
		},
		{
			name: "pending delete",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.deleteFailure = remoteCrawlPendingBucket
			},
		},
		{
			name: "expiry write",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putFailure = remoteCrawlLeaseExpiryBucket
			},
		},
		{
			name: "lease count write",
			prepare: func(engine *remoteCrawlFaultEngine) {
				engine.putFailure = remoteCrawlLeaseCountBucket
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broker, _, engine := openRemoteCrawlFaultBroker(t)
			stageURL(t, broker, testURLA)
			_, selected, err := broker.leaseCandidates(t.Context(), testPeerA)
			if err != nil {
				t.Fatal(err)
			}
			test.prepare(engine)
			if _, err := broker.claim(
				t.Context(),
				testPeerA,
				selected,
				time.Now().Add(time.Minute),
			); err == nil {
				t.Fatal("claim storage failure ignored")
			}
		})
	}
}

func TestRemoteCrawlClaimStopsAtAvailableSlots(t *testing.T) {
	broker, _, _ := openRemoteCrawlFaultBroker(t)
	stageURL(t, broker, testURLA)
	stageURL(t, broker, testURLB)
	_, selected, err := broker.leaseCandidates(t.Context(), testPeerA)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := broker.claim(
		t.Context(),
		testPeerA,
		selected,
		time.Now().Add(time.Minute),
	)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("bounded claim = %+v, %v", claimed, err)
	}
}

func TestRemoteCrawlReleaseReportsEveryDurableFailure(t *testing.T) {
	t.Run("non lease", func(t *testing.T) {
		broker, storage, _ := openRemoteCrawlFaultBroker(t)
		if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
			return broker.releaseLease(tx, queueRecord{State: queueStatePending})
		}); err != nil {
			t.Fatal(err)
		}
	})
	tests := []struct {
		name    string
		prepare func(*remoteCrawlFaultEngine, queueRecord)
	}{
		{
			name: "lease count read",
			prepare: func(engine *remoteCrawlFaultEngine, _ queueRecord) {
				engine.putRaw(remoteCrawlLeaseCountBucket, vault.Key(testPeerA.String()), []byte{1})
			},
		},
		{
			name: "missing lease count",
			prepare: func(engine *remoteCrawlFaultEngine, _ queueRecord) {
				engine.deleteRaw(remoteCrawlLeaseCountBucket, vault.Key(testPeerA.String()))
			},
		},
		{
			name: "lease count delete",
			prepare: func(engine *remoteCrawlFaultEngine, _ queueRecord) {
				engine.deleteFailure = remoteCrawlLeaseCountBucket
			},
		},
		{
			name: "lease count write",
			prepare: func(engine *remoteCrawlFaultEngine, _ queueRecord) {
				engine.putRaw(
					remoteCrawlLeaseCountBucket,
					vault.Key(testPeerA.String()),
					remoteCrawlRawUint64(2),
				)
				engine.putFailure = remoteCrawlLeaseCountBucket
			},
		},
		{
			name: "expiry delete",
			prepare: func(engine *remoteCrawlFaultEngine, _ queueRecord) {
				engine.deleteFailure = remoteCrawlLeaseExpiryBucket
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broker, storage, engine, record := remoteCrawlFaultLease(t)
			test.prepare(engine, record)
			if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
				return broker.releaseLease(tx, record)
			}); err == nil {
				t.Fatal("release storage failure ignored")
			}
		})
	}
}

func TestRemoteCrawlCompletedOrderReportsEveryDeleteFailure(t *testing.T) {
	tests := []struct {
		name   string
		bucket vault.Name
	}{
		{name: "order", bucket: remoteCrawlOrderBucket},
		{name: "URL sequence", bucket: remoteCrawlURLSequenceBucket},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broker, _, engine, record := remoteCrawlFaultLease(t)
			engine.deleteFailure = test.bucket
			if err := broker.deleteOrder(t.Context(), record); err == nil {
				t.Fatal("completed order delete failure ignored")
			}
		})
	}
	t.Run("release", func(t *testing.T) {
		broker, _, engine, record := remoteCrawlFaultLease(t)
		engine.putRaw(remoteCrawlLeaseCountBucket, vault.Key(testPeerA.String()), []byte{1})
		if err := broker.deleteOrder(t.Context(), record); err == nil {
			t.Fatal("completed order release failure ignored")
		}
	})
}

func TestRemoteCrawlRejectedOrderReportsEveryDeleteFailure(t *testing.T) {
	t.Run("stale candidate", func(t *testing.T) {
		broker, _, _ := openRemoteCrawlFaultBroker(t)
		if err := broker.deletePendingOrder(
			t.Context(),
			queueRecord{Sequence: 99, State: queueStatePending},
		); err != nil {
			t.Fatal(err)
		}
	})
	tests := []struct {
		name    string
		bucket  vault.Name
		corrupt bool
	}{
		{name: "order read", bucket: remoteCrawlOrderBucket, corrupt: true},
		{name: "order delete", bucket: remoteCrawlOrderBucket},
		{name: "pending delete", bucket: remoteCrawlPendingBucket},
		{name: "URL sequence delete", bucket: remoteCrawlURLSequenceBucket},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broker, _, engine := openRemoteCrawlFaultBroker(t)
			stageURL(t, broker, testURLA)
			hash, err := yagomodel.HashURL(testURLA)
			if err != nil {
				t.Fatal(err)
			}
			record := queueRecord{Sequence: 0, URLHash: hash.String(), State: queueStatePending}
			if test.corrupt {
				engine.putRaw(test.bucket, sequenceKey(0), []byte{1})
			} else {
				engine.deleteFailure = test.bucket
			}
			if err := broker.deletePendingOrder(t.Context(), record); err == nil {
				t.Fatal("rejected order delete failure ignored")
			}
		})
	}
}
