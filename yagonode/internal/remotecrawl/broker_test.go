package remotecrawl

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	testPeerA = yagomodel.Hash("AAAAAAAAAAAA")
	testPeerB = yagomodel.Hash("BBBBBBBBBBBB")
	testURLA  = "https://example.com/a"
	testURLB  = "https://example.com/b"
)

type recordingReceiver struct {
	mu      sync.Mutex
	rows    []yagomodel.URIMetadataRow
	receipt urlmeta.Receipt
	err     error
}

func (r *recordingReceiver) Receive(
	_ context.Context,
	rows []yagomodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, rows...)

	return r.receipt, r.err
}

func publicResolver(context.Context, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

func remoteConfig(now func() time.Time) Config {
	return Config{
		Enabled: true, TrustedPeers: []yagomodel.Hash{testPeerA},
		AllowedDestinations: []string{"example.com"},
		RequestsPerMinute:   2, OutstandingPerPeer: 1,
		LeaseTTL: time.Minute, QueueCapacity: 8,
		Resolver: publicResolver, Now: now,
	}
}

func openMemoryBroker(
	t *testing.T,
	config Config,
	receiver URLMetadataReceiver,
) (*Broker, *vault.Vault) {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open memory vault: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	broker, err := Open(config, storage, receiver)
	if err != nil {
		t.Fatalf("open remote crawl broker: %v", err)
	}

	return broker, storage
}

func stageURL(t *testing.T, broker *Broker, rawURL string) {
	t.Helper()
	if err := broker.StageOrder(t.Context(), yagocrawlcontract.CrawlOrder{
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL: rawURL, Mode: yagocrawlcontract.CrawlRequestModeURL,
			AppDate: time.Unix(10, 0), AnchorName: "title",
		}},
	}); err != nil {
		t.Fatalf("stage URL: %v", err)
	}
}

func metadataReceipt(
	t *testing.T,
	peer yagomodel.Hash,
	result string,
) yagoproto.CrawlReceiptRequest {
	t.Helper()
	hash, err := yagomodel.HashURL(testURLA)
	if err != nil {
		t.Fatal(err)
	}
	row := yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaHash: hash.String(),
		yagomodel.URLMetaURL:  yagomodel.EncodeBase64WireForm(testURLA),
	}}

	return yagoproto.CrawlReceiptRequest{
		Iam: peer, Result: result,
		LURLEntry: yagomodel.EncodeBase64WireForm(row.String()),
	}
}

func TestOpenDisabledDoesNotRegisterState(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	broker, err := Open(Config{}, storage, nil)
	if err != nil || broker != nil {
		t.Fatalf("Open(disabled) = %v, %v", broker, err)
	}
}

func TestOpenEnabledRequiresTrustDestinationAndReceiver(t *testing.T) {
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	for _, config := range []Config{
		{Enabled: true},
		{Enabled: true, TrustedPeers: []yagomodel.Hash{testPeerA}},
	} {
		if _, err := Open(config, storage, &recordingReceiver{}); err == nil {
			t.Fatalf("Open(%+v) succeeded", config)
		}
	}
	config := remoteConfig(time.Now)
	if _, err := Open(config, storage, nil); err == nil {
		t.Fatal("Open without receiver succeeded")
	}
}

func TestOpenBoundsPolicyLists(t *testing.T) {
	config := remoteConfig(time.Now)
	config.TrustedPeers = make([]yagomodel.Hash, MaximumTrustedPeers+1)
	for index := range config.TrustedPeers {
		config.TrustedPeers[index] = testPeerA
	}
	if _, err := Open(config, nil, &recordingReceiver{}); err == nil {
		t.Fatal("oversized trusted peer list accepted")
	}
	config = remoteConfig(time.Now)
	config.AllowedDestinations = make([]string, MaximumAllowedDestinations+1)
	for index := range config.AllowedDestinations {
		config.AllowedDestinations[index] = "example.com"
	}
	if _, err := Open(config, nil, &recordingReceiver{}); err == nil {
		t.Fatal("oversized destination list accepted")
	}
}

func TestLeaseRequiresTrustedPeerAndEnforcesOutstandingAndRate(t *testing.T) {
	now := time.Unix(100, 0)
	broker, _ := openMemoryBroker(
		t,
		remoteConfig(func() time.Time { return now }),
		&recordingReceiver{},
	)
	stageURL(t, broker, testURLA)
	stageURL(t, broker, testURLB)
	if _, err := broker.URLsForRemoteCrawl(
		t.Context(),
		testPeerB,
		1,
		time.Second,
	); !errors.Is(
		err,
		ErrPeerNotTrusted,
	) {
		t.Fatalf("untrusted lease error = %v", err)
	}
	first, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 10, time.Second)
	if err != nil || len(first) != 1 || first[0].Link != testURLA {
		t.Fatalf("first lease = %+v, %v", first, err)
	}
	second, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second)
	if err != nil || len(second) != 0 {
		t.Fatalf("outstanding-limited lease = %+v, %v", second, err)
	}
	if _, err := broker.URLsForRemoteCrawl(
		t.Context(),
		testPeerA,
		1,
		time.Second,
	); !errors.Is(
		err,
		ErrRateLimited,
	) {
		t.Fatalf("rate-limited lease error = %v", err)
	}
}

func TestConcurrentLeasesCannotExceedPeerOutstandingLimit(t *testing.T) {
	now := time.Unix(100, 0)
	config := remoteConfig(func() time.Time { return now })
	config.RequestsPerMinute = MaximumRequestsPerMinute
	broker, _ := openMemoryBroker(t, config, &recordingReceiver{})
	stageURL(t, broker, testURLA)
	stageURL(t, broker, testURLB)
	results := make(chan int, 2)
	failures := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			leased, err := broker.URLsForRemoteCrawl(
				context.Background(),
				testPeerA,
				1,
				time.Second,
			)
			results <- len(leased)
			failures <- err
		}()
	}
	group.Wait()
	close(results)
	close(failures)
	leased := 0
	for count := range results {
		leased += count
	}
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if leased != 1 {
		t.Fatalf("concurrent leases = %d, want 1", leased)
	}
}

func TestQueueCapacityBoundsDistinctURLsButAllowsDuplicates(t *testing.T) {
	now := time.Unix(100, 0)
	config := remoteConfig(func() time.Time { return now })
	config.QueueCapacity = 1
	broker, _ := openMemoryBroker(t, config, &recordingReceiver{})
	stageURL(t, broker, testURLA)
	stageURL(t, broker, testURLA)
	err := broker.StageOrder(t.Context(), yagocrawlcontract.CrawlOrder{
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL: testURLB, Mode: yagocrawlcontract.CrawlRequestModeURL,
		}},
	})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("second distinct URL error = %v", err)
	}
}

func TestFillReceiptMustMatchPeerLeaseAndURL(t *testing.T) {
	now := time.Unix(100, 0)
	receiver := &recordingReceiver{}
	config := remoteConfig(func() time.Time { return now })
	config.TrustedPeers = append(config.TrustedPeers, testPeerB)
	broker, _ := openMemoryBroker(t, config, receiver)
	stageURL(t, broker, testURLA)
	if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err != nil {
		t.Fatal(err)
	}
	wrongPeer, err := broker.ProcessReceipt(
		t.Context(),
		metadataReceipt(t, testPeerB, yagoproto.CrawlReceiptResultFill),
	)
	if err != nil || wrongPeer.Delay != ReceiptRetryDelay {
		t.Fatalf("wrong-peer receipt = %+v, %v", wrongPeer, err)
	}
	accepted, err := broker.ProcessReceipt(
		t.Context(),
		metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultFill),
	)
	if err != nil || accepted.Delay != ReceiptAcceptedDelay {
		t.Fatalf("accepted receipt = %+v, %v", accepted, err)
	}
	if len(receiver.rows) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(receiver.rows))
	}
	again, err := broker.ProcessReceipt(
		t.Context(),
		metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultFill),
	)
	if err != nil || again.Delay != ReceiptRetryDelay || len(receiver.rows) != 1 {
		t.Fatalf("replayed receipt = %+v, %v, rows=%d", again, err, len(receiver.rows))
	}
}

func TestFailureReceiptRequeuesWithoutStoringMetadata(t *testing.T) {
	now := time.Unix(100, 0)
	receiver := &recordingReceiver{}
	broker, _ := openMemoryBroker(t, remoteConfig(func() time.Time { return now }), receiver)
	stageURL(t, broker, testURLA)
	if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err != nil {
		t.Fatal(err)
	}
	response, err := broker.ProcessReceipt(
		t.Context(),
		metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultRobot),
	)
	if err != nil || response.Delay != ReceiptRetryDelay || len(receiver.rows) != 0 {
		t.Fatalf("failure receipt = %+v, %v, rows=%d", response, err, len(receiver.rows))
	}
	now = now.Add(time.Minute)
	leased, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second)
	if err != nil || len(leased) != 1 || leased[0].Link != testURLA {
		t.Fatalf("requeued lease = %+v, %v", leased, err)
	}
}

func TestMetadataStoreFailureReturnsRetryAndRequeues(t *testing.T) {
	now := time.Unix(100, 0)
	receiver := &recordingReceiver{err: errors.New("storage unavailable")}
	config := remoteConfig(func() time.Time { return now })
	broker, _ := openMemoryBroker(t, config, receiver)
	stageURL(t, broker, testURLA)
	if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err != nil {
		t.Fatal(err)
	}
	response, err := broker.ProcessReceipt(
		t.Context(),
		metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultFill),
	)
	if err != nil || response.Delay != ReceiptRetryDelay {
		t.Fatalf("failed-store receipt = %+v, %v", response, err)
	}
	receiver.err = nil
	leased, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second)
	if err != nil || len(leased) != 1 || leased[0].Link != testURLA {
		t.Fatalf("requeued lease = %+v, %v", leased, err)
	}
}

func TestLeaseDestinationValidationDoesNotBlockReceipt(t *testing.T) {
	var blockOther atomic.Bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	resolver := func(ctx context.Context, host string) ([]netip.Addr, error) {
		if host == "other.example" && blockOther.Load() {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		return publicResolver(ctx, host)
	}
	now := time.Unix(100, 0)
	config := remoteConfig(func() time.Time { return now })
	config.AllowedDestinations = []string{"example.com", "other.example"}
	config.OutstandingPerPeer = 2
	config.Resolver = resolver
	receiver := &recordingReceiver{}
	broker, _ := openMemoryBroker(t, config, receiver)
	stageURL(t, broker, testURLA)
	stageURL(t, broker, "https://other.example/b")
	if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err != nil {
		t.Fatal(err)
	}
	blockOther.Store(true)
	leaseDone := make(chan error, 1)
	go func() {
		_, err := broker.URLsForRemoteCrawl(context.Background(), testPeerA, 1, time.Second)
		leaseDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("destination validation did not start")
	}
	receiptRequest := metadataReceipt(
		t,
		testPeerA,
		yagoproto.CrawlReceiptResultFill,
	)
	receiptDone := make(chan error, 1)
	go func() {
		response, err := broker.ProcessReceipt(
			context.Background(),
			receiptRequest,
		)
		if err == nil && response.Delay != ReceiptAcceptedDelay {
			err = errors.New("receipt was not accepted")
		}
		receiptDone <- err
	}()
	select {
	case err := <-receiptDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("destination validation blocked receipt")
	}
	close(release)
	if err := <-leaseDone; err != nil {
		t.Fatal(err)
	}
}

func TestReceiptDestinationValidationDoesNotBlockUnrelatedLease(t *testing.T) {
	var blockReceipt atomic.Bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	resolver := func(ctx context.Context, host string) ([]netip.Addr, error) {
		if host == "example.com" && blockReceipt.Load() {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		return publicResolver(ctx, host)
	}
	now := time.Unix(100, 0)
	config := remoteConfig(func() time.Time { return now })
	config.AllowedDestinations = []string{"example.com", "other.example"}
	config.OutstandingPerPeer = 2
	config.Resolver = resolver
	broker, _ := openMemoryBroker(t, config, &recordingReceiver{})
	stageURL(t, broker, testURLA)
	stageURL(t, broker, "https://other.example/b")
	if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err != nil {
		t.Fatal(err)
	}
	blockReceipt.Store(true)
	receiptDone := make(chan error, 1)
	go func() {
		response, err := broker.ProcessReceipt(
			context.Background(),
			metadataReceipt(t, testPeerA, yagoproto.CrawlReceiptResultFill),
		)
		if err == nil && response.Delay != ReceiptAcceptedDelay {
			err = errors.New("receipt was not accepted")
		}
		receiptDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("receipt destination validation did not start")
	}
	leased, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second)
	if err != nil || len(leased) != 1 || leased[0].Link != "https://other.example/b" {
		t.Fatalf("unrelated lease = %+v, %v", leased, err)
	}
	close(release)
	if err := <-receiptDone; err != nil {
		t.Fatal(err)
	}
}

func TestDurableLeaseExpiresAndRequeuesAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote-crawl.db")
	var clock atomic.Int64
	clock.Store(time.Unix(100, 0).UnixNano())
	now := func() time.Time { return time.Unix(0, clock.Load()) }
	config := remoteConfig(now)
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := Open(config, storage, &recordingReceiver{})
	if err != nil {
		t.Fatal(err)
	}
	stageURL(t, broker, testURLA)
	if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	clock.Store(time.Unix(100, 0).Add(2 * time.Minute).UnixNano())
	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	broker, err = Open(config, storage, &recordingReceiver{})
	if err != nil {
		t.Fatal(err)
	}
	leased, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second)
	if err != nil || len(leased) != 1 || leased[0].Link != testURLA {
		t.Fatalf("restarted lease = %+v, %v", leased, err)
	}
}

func TestQueueStateUpgradeReconcilesMixedDurableState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote-crawl-index-upgrade.db")
	now := time.Unix(100, 0)
	leaseUntil, collections := writeQueueStateUpgradeFixture(t, path, now)
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	config := remoteConfig(func() time.Time { return now })
	config.OutstandingPerPeer = 2
	broker, err := Open(config, storage, &recordingReceiver{})
	if err != nil {
		t.Fatal(err)
	}
	outstanding, pending, err := broker.leaseCandidates(t.Context(), testPeerA)
	if err != nil || outstanding != 1 || len(pending) != 1 || pending[0].URL != testURLA {
		t.Fatalf("reconciled candidates = %d, %+v, %v", outstanding, pending, err)
	}
	assertQueueStateUpgrade(t, storage, collections, leaseUntil)
	leased, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second)
	if err != nil || len(leased) != 1 || leased[0].Link != testURLA {
		t.Fatalf("upgraded pending lease = %+v, %v", leased, err)
	}
}

func TestLeasePreparationReadsOnlyBoundedPendingWindow(t *testing.T) {
	config := remoteConfig(time.Now)
	config.QueueCapacity = 128
	broker, storage := openMemoryBroker(t, config, &recordingReceiver{})
	for index := range MaximumRemoteCrawlBatch {
		stageURL(t, broker, fmt.Sprintf("https://example.com/%03d", index))
	}
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return broker.pending.Put(
			tx,
			sequenceKey(1000),
			pendingRecord{Sequence: 1000},
		)
	}); err != nil {
		t.Fatal(err)
	}
	_, pending, err := broker.leaseCandidates(t.Context(), testPeerA)
	if err != nil || len(pending) != MaximumRemoteCrawlBatch {
		t.Fatalf("bounded candidates = %d, %v", len(pending), err)
	}
}

func TestPeerRequestRateSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote-crawl-rate.db")
	now := func() time.Time { return time.Unix(100, 0) }
	config := remoteConfig(now)
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := Open(config, storage, &recordingReceiver{})
	if err != nil {
		t.Fatal(err)
	}
	for range config.RequestsPerMinute {
		if _, err := broker.URLsForRemoteCrawl(t.Context(), testPeerA, 1, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	broker, err = Open(config, storage, &recordingReceiver{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.URLsForRemoteCrawl(
		t.Context(),
		testPeerA,
		1,
		time.Second,
	); !errors.Is(
		err,
		ErrRateLimited,
	) {
		t.Fatalf("restarted request rate error = %v", err)
	}
}

func TestConcurrentStagingAndLeasingIsRaceSafe(t *testing.T) {
	now := time.Unix(100, 0)
	config := remoteConfig(func() time.Time { return now })
	config.OutstandingPerPeer = MaximumOutstandingPerPeer
	config.RequestsPerMinute = MaximumRequestsPerMinute
	config.QueueCapacity = 128
	broker, _ := openMemoryBroker(t, config, &recordingReceiver{})
	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for item := 0; item < 20; item++ {
				rawURL := fmt.Sprintf("%s?worker=%d&item=%d", testURLA, worker, item)
				_ = broker.StageOrder(context.Background(), yagocrawlcontract.CrawlOrder{
					Requests: []yagocrawlcontract.CrawlRequest{{URL: rawURL}},
				})
				_, _ = broker.URLsForRemoteCrawl(context.Background(), testPeerA, 1, time.Second)
			}
		}(worker)
	}
	group.Wait()
}
