package remotecrawl

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagoproto"
)

func TestReceiptMetadataRejectsWireAndRowDecodeFailures(t *testing.T) {
	if _, _, _, err := parseReceiptMetadata(t.Context(), "b|@@@"); err == nil {
		t.Fatal("invalid metadata wire form accepted")
	}
	if _, _, _, err := parseReceiptMetadata(
		t.Context(),
		yagomodel.EncodeBase64WireForm("{}"),
	); err == nil {
		t.Fatal("invalid metadata row accepted")
	}
	hash, err := yagomodel.HashURL(testURLA)
	if err != nil {
		t.Fatal(err)
	}
	row := yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaHash: hash.String(),
		yagomodel.URLMetaURL:  "b|@@@",
	}}
	if _, _, _, err := parseReceiptMetadata(
		t.Context(),
		yagomodel.EncodeBase64WireForm(row.String()),
	); err == nil {
		t.Fatal("invalid metadata URL wire form accepted")
	}
}

func TestRemoteCrawlReceiptSurfacesEveryLeaseMutationFailure(t *testing.T) {
	t.Run("lease lookup", func(t *testing.T) {
		broker, _, engine, _ := remoteCrawlFaultLease(t)
		hash, err := yagomodel.HashURL(testURLA)
		if err != nil {
			t.Fatal(err)
		}
		engine.putRaw(remoteCrawlURLSequenceBucket, vault.Key(hash.String()), []byte{1})
		if _, err := broker.ProcessReceipt(
			t.Context(),
			metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultFill),
		); err == nil {
			t.Fatal("lease lookup failure ignored")
		}
	})
	t.Run("policy requeue", func(t *testing.T) {
		broker, _, engine, _ := remoteCrawlFaultLease(t)
		broker.policy.resolver = func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		engine.deleteFailure = remoteCrawlLeaseCountBucket
		if _, err := broker.ProcessReceipt(
			t.Context(),
			metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultFill),
		); err == nil {
			t.Fatal("policy requeue failure ignored")
		}
	})
	t.Run("result requeue", func(t *testing.T) {
		broker, _, engine, _ := remoteCrawlFaultLease(t)
		engine.deleteFailure = remoteCrawlLeaseCountBucket
		if _, err := broker.ProcessReceipt(
			t.Context(),
			metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultRobot),
		); err == nil {
			t.Fatal("result requeue failure ignored")
		}
	})
	t.Run("store requeue", func(t *testing.T) {
		broker, _, engine, _ := remoteCrawlFaultLease(t)
		broker.receiver = &recordingReceiver{receipt: urlmeta.Receipt{Busy: true}}
		engine.deleteFailure = remoteCrawlLeaseCountBucket
		if _, err := broker.ProcessReceipt(
			t.Context(),
			metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultFill),
		); err == nil {
			t.Fatal("store requeue failure ignored")
		}
	})
	t.Run("completion delete", func(t *testing.T) {
		broker, _, engine, _ := remoteCrawlFaultLease(t)
		engine.deleteFailure = remoteCrawlOrderBucket
		if _, err := broker.ProcessReceipt(
			t.Context(),
			metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultFill),
		); err == nil {
			t.Fatal("completion delete failure ignored")
		}
	})
}

func TestRemoteCrawlLeaseLookupRejectsCorruptDurableState(t *testing.T) {
	t.Run("URL sequence", func(t *testing.T) {
		broker, _, engine := openRemoteCrawlFaultBroker(t)
		engine.putRaw(remoteCrawlURLSequenceBucket, vault.Key(testPeerA.String()), []byte{1})
		if _, _, err := broker.leasedRecord(
			t.Context(),
			testPeerA,
			testPeerA,
			time.Now(),
		); err == nil {
			t.Fatal("corrupt URL sequence accepted")
		}
	})
	t.Run("order", func(t *testing.T) {
		broker, _, engine := openRemoteCrawlFaultBroker(t)
		engine.putRaw(
			remoteCrawlURLSequenceBucket,
			vault.Key(testPeerA.String()),
			remoteCrawlRawUint64(1),
		)
		engine.putRaw(remoteCrawlOrderBucket, sequenceKey(1), []byte{1})
		if _, _, err := broker.leasedRecord(
			t.Context(),
			testPeerA,
			testPeerA,
			time.Now(),
		); err == nil {
			t.Fatal("corrupt lease accepted")
		}
	})
	t.Run("view", func(t *testing.T) {
		broker, _, engine := openRemoteCrawlFaultBroker(t)
		engine.viewFailure = true
		if _, _, err := broker.leasedRecord(
			t.Context(),
			testPeerA,
			testPeerA,
			time.Now(),
		); err == nil {
			t.Fatal("lease view failure ignored")
		}
	})
}

func TestRemoteCrawlLeaseLookupExpiresItsRecord(t *testing.T) {
	broker, _, _, record := remoteCrawlFaultLease(t)
	foundRecord, found, err := broker.leasedRecord(
		t.Context(),
		testPeerA,
		yagomodel.Hash(record.URLHash),
		time.Unix(0, record.LeaseUntil).Add(time.Second),
	)
	if err != nil || found || foundRecord != (queueRecord{}) {
		t.Fatalf("expired lease = %+v, %t, %v", foundRecord, found, err)
	}
}

func TestRemoteCrawlLeaseLookupReportsExpirationRequeueFailure(t *testing.T) {
	broker, _, engine, record := remoteCrawlFaultLease(t)
	engine.deleteFailure = remoteCrawlLeaseCountBucket
	if _, _, err := broker.leasedRecord(
		t.Context(),
		testPeerA,
		yagomodel.Hash(record.URLHash),
		time.Unix(0, record.LeaseUntil).Add(time.Second),
	); err == nil {
		t.Fatal("expiration requeue failure ignored")
	}
}

func TestRemoteCrawlRequeueReportsEveryDurableWriteFailure(t *testing.T) {
	tests := []struct {
		name   string
		bucket vault.Name
	}{
		{name: "order", bucket: remoteCrawlOrderBucket},
		{name: "pending", bucket: remoteCrawlPendingBucket},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broker, _, engine, record := remoteCrawlFaultLease(t)
			engine.putFailure = test.bucket
			if err := broker.requeueRecord(t.Context(), record); err == nil {
				t.Fatal("requeue write failure ignored")
			}
		})
	}
}
