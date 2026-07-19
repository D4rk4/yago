package remotecrawl

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRemoteCrawlPeerRateReportsDurableFailures(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		broker, _, engine := openRemoteCrawlFaultBroker(t)
		engine.putRaw(remoteCrawlRequestRateBucket, vault.Key(testPeerA.String()), []byte{1})
		if err := broker.consumePeerRequest(t.Context(), testPeerA, time.Now()); err == nil {
			t.Fatal("rate read failure ignored")
		}
	})
	t.Run("write", func(t *testing.T) {
		broker, _, engine := openRemoteCrawlFaultBroker(t)
		engine.putFailure = remoteCrawlRequestRateBucket
		if err := broker.consumePeerRequest(t.Context(), testPeerA, time.Now()); err == nil {
			t.Fatal("rate write failure ignored")
		}
	})
}

func TestRemoteCrawlExpiredLeaseReportsEveryDurableFailure(t *testing.T) {
	t.Run("scan", func(t *testing.T) {
		broker, _, engine := openRemoteCrawlFaultBroker(t)
		engine.scanFailure = remoteCrawlLeaseExpiryBucket
		if err := broker.requeueExpired(t.Context(), time.Now()); err == nil {
			t.Fatal("expiry scan failure ignored")
		}
	})
	t.Run("order read", func(t *testing.T) {
		broker, _, engine, record := remoteCrawlFaultLease(t)
		engine.putRaw(remoteCrawlOrderBucket, sequenceKey(record.Sequence), []byte{1})
		if err := broker.requeueExpired(
			t.Context(),
			time.Unix(0, record.LeaseUntil).Add(time.Second),
		); err == nil {
			t.Fatal("expired order read failure ignored")
		}
	})
	t.Run("stale expiry delete", func(t *testing.T) {
		broker, storage, engine := openRemoteCrawlFaultBroker(t)
		key := leaseExpiryKey(1, 7)
		if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
			return broker.leaseExpiries.Put(
				tx,
				key,
				leaseExpiryRecord{Sequence: 7},
			)
		}); err != nil {
			t.Fatal(err)
		}
		engine.deleteFailure = remoteCrawlLeaseExpiryBucket
		if err := broker.requeueExpired(t.Context(), time.Now()); err == nil {
			t.Fatal("stale expiry delete failure ignored")
		}
	})
	t.Run("release", func(t *testing.T) {
		broker, _, engine, record := remoteCrawlFaultLease(t)
		engine.putRaw(remoteCrawlLeaseCountBucket, vault.Key(testPeerA.String()), []byte{1})
		if err := broker.requeueExpired(
			t.Context(),
			time.Unix(0, record.LeaseUntil).Add(time.Second),
		); err == nil {
			t.Fatal("expired release failure ignored")
		}
	})
	t.Run("order write", func(t *testing.T) {
		broker, _, engine, record := remoteCrawlFaultLease(t)
		engine.putFailure = remoteCrawlOrderBucket
		if err := broker.requeueExpired(
			t.Context(),
			time.Unix(0, record.LeaseUntil).Add(time.Second),
		); err == nil {
			t.Fatal("expired order write failure ignored")
		}
	})
	t.Run("pending write", func(t *testing.T) {
		broker, _, engine, record := remoteCrawlFaultLease(t)
		engine.putFailure = remoteCrawlPendingBucket
		if err := broker.requeueExpired(
			t.Context(),
			time.Unix(0, record.LeaseUntil).Add(time.Second),
		); err == nil {
			t.Fatal("expired pending write failure ignored")
		}
	})
}
