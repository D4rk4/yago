package remotecrawl

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagoproto"
)

func TestOpenRemoteCrawlRejectsMissingAndMalformedDependencies(t *testing.T) {
	config := remoteConfig(time.Now)
	if _, err := Open(config, nil, &recordingReceiver{}); err == nil {
		t.Fatal("missing storage accepted")
	}
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	config.AllowedDestinations = []string{"not_a_domain"}
	if _, err := Open(config, storage, &recordingReceiver{}); err == nil {
		t.Fatal("invalid destination accepted")
	}
	config = remoteConfig(time.Now)
	config.TrustedPeers = []yagomodel.Hash{"short"}
	if _, err := Open(config, storage, &recordingReceiver{}); err == nil {
		t.Fatal("invalid trusted peer accepted")
	}
}

func TestRemoteCrawlLeaseCountBoundariesAndEmptyQueue(t *testing.T) {
	config := remoteConfig(time.Now)
	config.OutstandingPerPeer = MaximumOutstandingPerPeer
	broker, _ := openMemoryBroker(t, config, &recordingReceiver{})
	if records, err := broker.URLsForRemoteCrawl(
		t.Context(),
		testPeerA,
		0,
		time.Second,
	); err != nil ||
		records != nil {
		t.Fatalf("zero lease = %+v, %v", records, err)
	}
	if records, err := broker.URLsForRemoteCrawl(
		t.Context(),
		testPeerA,
		MaximumRemoteCrawlBatch+1,
		time.Second,
	); err != nil || len(records) != 0 {
		t.Fatalf("empty bounded lease = %+v, %v", records, err)
	}
}

func TestRemoteCrawlStageRejectsUnsupportedAndUnsafeRequests(t *testing.T) {
	recorder := &observationRecorder{}
	broker, _ := openMemoryBroker(t, remoteConfig(time.Now), &recordingReceiver{})
	broker.observers = []Observer{recorder}
	err := broker.StageOrder(t.Context(), yagocrawlcontract.CrawlOrder{
		Requests: []yagocrawlcontract.CrawlRequest{
			{URL: testURLA, Mode: "unsupported"},
			{URL: "http://127.0.0.1/a", Mode: yagocrawlcontract.CrawlRequestModeURL},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.observations) != 2 ||
		recorder.observations[0].Outcome != "destination_rejected" ||
		recorder.observations[1].Count != 0 {
		t.Fatalf("stage observations = %+v", recorder.observations)
	}
}

func TestRemoteCrawlLeaseDeletesDestinationThatBecomesUnsafe(t *testing.T) {
	var unsafe atomic.Bool
	resolver := func(context.Context, string) ([]netip.Addr, error) {
		if unsafe.Load() {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}

		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	config := remoteConfig(time.Now)
	config.Resolver = resolver
	broker, _ := openMemoryBroker(t, config, &recordingReceiver{})
	stageURL(t, broker, testURLA)
	unsafe.Store(true)
	leased, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second)
	if err != nil || len(leased) != 0 {
		t.Fatalf("unsafe lease = %+v, %v", leased, err)
	}
	unsafe.Store(false)
	stageURL(t, broker, testURLA)
	leased, err = broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second)
	if err != nil || len(leased) != 1 {
		t.Fatalf("restaged lease = %+v, %v", leased, err)
	}
}

func TestRemoteCrawlReceiptRequeuesDestinationThatBecomesUnsafe(t *testing.T) {
	var unsafe atomic.Bool
	resolver := func(context.Context, string) ([]netip.Addr, error) {
		if unsafe.Load() {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}

		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	now := time.Unix(100, 0)
	config := remoteConfig(func() time.Time { return now })
	config.Resolver = resolver
	broker, _ := openMemoryBroker(t, config, &recordingReceiver{})
	stageURL(t, broker, testURLA)
	if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err != nil {
		t.Fatal(err)
	}
	unsafe.Store(true)
	response, err := broker.ProcessReceipt(
		t.Context(),
		metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultFill),
	)
	if err != nil || response.Delay != ReceiptPolicyDelay {
		t.Fatalf("unsafe receipt = %+v, %v", response, err)
	}
}

func TestRemoteCrawlReceiptValidatesEnvelopeAndMetadata(t *testing.T) {
	broker, _ := openMemoryBroker(t, remoteConfig(time.Now), &recordingReceiver{})
	requests := []yagoproto.CrawlReceiptRequest{
		{Iam: testPeerB, Result: yagoproto.CrawlReceiptResultFill},
		{Iam: testPeerA, Result: "unsupported", LURLEntry: "value"},
		{Iam: testPeerA, Result: yagoproto.CrawlReceiptResultFill},
		{Iam: testPeerA, Result: yagoproto.CrawlReceiptResultFill, LURLEntry: "%"},
	}
	for _, request := range requests {
		response, err := broker.ProcessReceipt(t.Context(), request)
		if err != nil || response.Delay != ReceiptRetryDelay {
			t.Fatalf("rejected receipt = %+v, %v", response, err)
		}
	}
}

func TestReceiptMetadataRejectsStructuralMismatches(t *testing.T) {
	hash, err := yagomodel.HashURL(testURLA)
	if err != nil {
		t.Fatal(err)
	}
	rows := []yagomodel.URIMetadataRow{
		{Properties: map[string]string{}},
		{Properties: map[string]string{
			yagomodel.URLMetaHash: hash.String(),
			yagomodel.URLMetaURL:  "%",
		}},
		{Properties: map[string]string{
			yagomodel.URLMetaHash: "short",
			yagomodel.URLMetaURL:  yagomodel.EncodeBase64WireForm(testURLA),
		}},
		{Properties: map[string]string{
			yagomodel.URLMetaHash: hash.String(),
			yagomodel.URLMetaURL:  yagomodel.EncodeBase64WireForm(testURLB),
		}},
	}
	rawValues := make([]string, 0, 2+len(rows))
	rawValues = append(rawValues,
		strings.Repeat("x", MaximumReceiptMetadataBytes+1),
		yagomodel.EncodeBase64WireForm("not a metadata row"),
	)
	for _, row := range rows {
		rawValues = append(rawValues, yagomodel.EncodeBase64WireForm(row.String()))
	}
	for _, raw := range rawValues {
		if _, _, _, err := parseReceiptMetadata(t.Context(), raw); err == nil {
			t.Fatalf("metadata %q accepted", raw)
		}
	}
}

func TestRemoteCrawlReceiptRequeuesBusyAndRejectedStorage(t *testing.T) {
	for _, test := range []struct {
		name    string
		receipt urlmeta.Receipt
	}{
		{name: "busy", receipt: urlmeta.Receipt{Busy: true}},
		{name: "rejected URL", receipt: urlmeta.Receipt{ErrorURL: []yagomodel.Hash{testPeerA}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Unix(100, 0)
			receiver := &recordingReceiver{receipt: test.receipt}
			broker, _ := openMemoryBroker(
				t,
				remoteConfig(func() time.Time { return now }),
				receiver,
			)
			stageURL(t, broker, testURLA)
			if _, err := broker.URLsForRemoteCrawl(
				t.Context(),
				testPeerA,
				1,
				time.Second,
			); err != nil {
				t.Fatal(err)
			}
			response, err := broker.ProcessReceipt(
				t.Context(),
				metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultFill),
			)
			if err != nil || response.Delay != ReceiptRetryDelay {
				t.Fatalf("store receipt = %+v, %v", response, err)
			}
		})
	}
}

func TestRemoteCrawlRateWindowHandlesClockChanges(t *testing.T) {
	broker, storage := openMemoryBroker(t, remoteConfig(time.Now), &recordingReceiver{})
	first := time.Unix(100, 0)
	if err := broker.consumePeerRequest(t.Context(), testPeerA, first); err != nil {
		t.Fatal(err)
	}
	for _, now := range []time.Time{first.Add(time.Minute), first.Add(-time.Minute)} {
		if err := broker.consumePeerRequest(t.Context(), testPeerA, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		rate, found, err := broker.requestRates.Get(tx, vault.Key(testPeerA.String()))
		if err != nil || !found || rate.Requests != 1 {
			t.Fatalf("rate = %+v, %t, %v", rate, found, err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCrawlExpiredLeaseIndexRejectsMalformedAndRemovesStaleEntries(t *testing.T) {
	broker, storage := openMemoryBroker(t, remoteConfig(time.Now), &recordingReceiver{})
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return broker.leaseExpiries.Put(
			tx,
			vault.Key("malformed"),
			leaseExpiryRecord{Sequence: 1},
		)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.expiredLeases(t.Context(), time.Now()); err == nil {
		t.Fatal("malformed expiry accepted")
	}
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := broker.leaseExpiries.Delete(tx, vault.Key("malformed"))
		if err != nil {
			return fmt.Errorf("delete malformed lease expiry: %w", err)
		}

		return broker.leaseExpiries.Put(
			tx,
			leaseExpiryKey(1, 7),
			leaseExpiryRecord{Sequence: 7},
		)
	}); err != nil {
		t.Fatal(err)
	}
	if err := broker.requeueExpired(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCrawlClaimSkipsAStaleSelection(t *testing.T) {
	broker, _ := openMemoryBroker(t, remoteConfig(time.Now), &recordingReceiver{})
	claimed, err := broker.claim(
		t.Context(),
		testPeerA,
		[]queueRecord{{Sequence: 999, State: queueStatePending}},
		time.Now().Add(time.Minute),
	)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("stale claim = %+v, %v", claimed, err)
	}
}

func TestRemoteCrawlContextCancellationStopsStorageWork(t *testing.T) {
	broker, _ := openMemoryBroker(t, remoteConfig(time.Now), &recordingReceiver{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := broker.URLsForRemoteCrawl(
		ctx,
		testPeerA,
		1,
		time.Second,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("canceled lease error = %v", err)
	}
}
