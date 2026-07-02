package yagonode

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/crawlbroker"
	"github.com/D4rk4/yago/yacynode/internal/dhtexchange"
	"github.com/D4rk4/yago/yacynode/internal/documentstore"
	"github.com/D4rk4/yago/yacynode/internal/eviction"
	"github.com/D4rk4/yago/yacynode/internal/memvault"
	"github.com/D4rk4/yago/yacynode/internal/metrics"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacynode/internal/nodestatus"
	"github.com/D4rk4/yago/yacynode/internal/peermessage"
	"github.com/D4rk4/yago/yacynode/internal/peernews"
	"github.com/D4rk4/yago/yacynode/internal/peerroster"
	"github.com/D4rk4/yago/yacynode/internal/rwi"
	"github.com/D4rk4/yago/yacynode/internal/searchindex"
	"github.com/D4rk4/yago/yacynode/internal/transfertally"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
	"github.com/D4rk4/yago/yacynode/internal/urlmetastaleness"
	"github.com/D4rk4/yago/yacynode/internal/urlreferences"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type exitStatus int

type fakeAnnouncer struct{}

func (fakeAnnouncer) Run(ctx context.Context) { <-ctx.Done() }

type scriptedSweeper struct {
	result   eviction.Result
	err      error
	calls    atomic.Int32
	called   chan int32
	cancel   context.CancelFunc
	cancelOn int32
}

type scriptedDHTOutboundCycle struct {
	receipt  dhtexchange.ScheduledDistributionReceipt
	err      error
	calls    atomic.Int32
	called   chan int32
	cancel   context.CancelFunc
	cancelOn int32
}

func (s *scriptedDHTOutboundCycle) RunOnce(
	context.Context,
) (dhtexchange.ScheduledDistributionReceipt, error) {
	call := s.calls.Add(1)
	if s.called != nil {
		s.called <- call
	}
	if s.cancel != nil && s.cancelOn == call {
		s.cancel()
	}

	return s.receipt, s.err
}

func (s *scriptedSweeper) Sweep(context.Context) (eviction.Result, error) {
	call := s.calls.Add(1)
	if s.called != nil {
		s.called <- call
	}
	if s.cancel != nil && s.cancelOn == call {
		s.cancel()
	}

	return s.result, s.err
}

type recordingCrawl struct {
	mounted atomic.Bool
	ran     atomic.Bool
	closed  atomic.Bool
}

func (r *recordingCrawl) mountDispatch(*http.ServeMux) { r.mounted.Store(true) }

func (r *recordingCrawl) Run(ctx context.Context) {
	r.ran.Store(true)
	<-ctx.Done()
}

func (r *recordingCrawl) Close() { r.closed.Store(true) }

type fakeRoster struct{}

func (fakeRoster) Discover(context.Context, ...yacymodel.Seed) {}

func (fakeRoster) ConfirmReachable(context.Context, yacymodel.Hash) {}

func (fakeRoster) ConfirmUnreachable(context.Context, yacymodel.Hash) {}

func (fakeRoster) RejectRemoteIndex(context.Context, yacymodel.Seed) {}

func (fakeRoster) FreshestPeers(context.Context, int) []yacymodel.Seed { return nil }

func (fakeRoster) ReachablePeers(context.Context) []yacymodel.Seed { return nil }

func (fakeRoster) KnownPeerCount(context.Context) int { return 0 }

func (fakeRoster) ReachablePeerCount(context.Context) int { return 0 }

type fakeSeedNews struct{}

func (fakeSeedNews) SeedNews(context.Context) string { return "" }

type fakeTransferTotals struct{}

func (fakeTransferTotals) TransferTotals(context.Context) nodestatus.TransferTotals {
	return nodestatus.TransferTotals{}
}

type reachableRoster struct {
	peers []yacymodel.Seed
}

func (r reachableRoster) Discover(context.Context, ...yacymodel.Seed) {}

func (r reachableRoster) ConfirmReachable(context.Context, yacymodel.Hash) {}

func (r reachableRoster) ConfirmUnreachable(context.Context, yacymodel.Hash) {}

func (r reachableRoster) RejectRemoteIndex(context.Context, yacymodel.Seed) {}

func (r reachableRoster) FreshestPeers(context.Context, int) []yacymodel.Seed { return r.peers }

func (r reachableRoster) ReachablePeers(context.Context) []yacymodel.Seed { return r.peers }

func (r reachableRoster) KnownPeerCount(context.Context) int { return len(r.peers) }

func (r reachableRoster) ReachablePeerCount(context.Context) int { return len(r.peers) }

type capacityProbe struct {
	atCapacity bool
	err        error
}

func (p capacityProbe) AtCapacity(context.Context) (bool, error) {
	return p.atCapacity, p.err
}

type rwiCounter struct {
	count int
	err   error
}

func (c rwiCounter) RWICount(context.Context) (int, error) {
	return c.count, c.err
}

func (c rwiCounter) RWIURLCount(context.Context, yacymodel.Hash) (int, error) {
	return 0, c.err
}

type publicReachabilityScript struct {
	reachable bool
	calls     atomic.Int32
}

func (s *publicReachabilityScript) Reachable(context.Context) bool {
	s.calls.Add(1)

	return s.reachable
}

type postingIndexOnly struct{}

func (postingIndexOnly) RWICount(context.Context) (int, error) { return 0, nil }

func (postingIndexOnly) RWIURLCount(context.Context, yacymodel.Hash) (int, error) {
	return 0, nil
}

func (postingIndexOnly) ScanWord(
	context.Context,
	yacymodel.Hash,
	func(yacymodel.RWIPosting) (bool, error),
) error {
	return nil
}

type failingCloser struct{}

func (failingCloser) Close() error { return errors.New("close failed") }

func restoreMainSeams(t *testing.T) {
	t.Helper()
	oldExitProcess := exitProcess
	oldRunNode := runNode
	oldOpenRuntimeVault := openRuntimeVault
	oldAssembleRuntimeNode := assembleRuntimeNode
	oldServeRuntimeNode := serveRuntimeNode
	oldListenAndServeHTTP := listenAndServeHTTP
	oldShutdownHTTPServer := shutdownHTTPServer
	t.Cleanup(func() {
		exitProcess = oldExitProcess
		runNode = oldRunNode
		openRuntimeVault = oldOpenRuntimeVault
		assembleRuntimeNode = oldAssembleRuntimeNode
		serveRuntimeNode = oldServeRuntimeNode
		listenAndServeHTTP = oldListenAndServeHTTP
		shutdownHTTPServer = oldShutdownHTTPServer
	})
}

func restoreAssemblySeams(t *testing.T) {
	t.Helper()
	oldOpenRuntimeNodeStorage := openRuntimeNodeStorage
	oldOpenRuntimePeerBirthDate := openRuntimePeerBirthDate
	oldOpenRuntimePeerNews := openRuntimePeerNews
	oldOpenRuntimeTransferTally := openRuntimeTransferTally
	oldAssembleRuntimePeerExchange := assembleRuntimePeerExchange
	oldBuildRuntimeDHTOutbound := buildRuntimeDHTOutbound
	oldBuildRuntimeCrawl := buildRuntimeCrawl
	t.Cleanup(func() {
		openRuntimeNodeStorage = oldOpenRuntimeNodeStorage
		openRuntimePeerBirthDate = oldOpenRuntimePeerBirthDate
		openRuntimePeerNews = oldOpenRuntimePeerNews
		openRuntimeTransferTally = oldOpenRuntimeTransferTally
		assembleRuntimePeerExchange = oldAssembleRuntimePeerExchange
		buildRuntimeDHTOutbound = oldBuildRuntimeDHTOutbound
		buildRuntimeCrawl = oldBuildRuntimeCrawl
	})
}

func restoreStorageSeams(t *testing.T) {
	t.Helper()
	oldOpenDocuments := openDocuments
	oldOpenSearchIndex := openSearchIndex
	oldOpenStalenessRanking := openStalenessRanking
	oldOpenURLMetadata := openURLMetadata
	oldOpenURLReferences := openURLReferences
	oldOpenRWIStorage := openRWIStorage
	t.Cleanup(func() {
		openDocuments = oldOpenDocuments
		openSearchIndex = oldOpenSearchIndex
		openStalenessRanking = oldOpenStalenessRanking
		openURLMetadata = oldOpenURLMetadata
		openURLReferences = oldOpenURLReferences
		openRWIStorage = oldOpenRWIStorage
	})
}

func restorePeerExchangeSeams(t *testing.T) {
	t.Helper()
	oldOpenPeerRoster := openPeerRoster
	oldOpenPeerMailbox := openPeerMailbox
	t.Cleanup(func() {
		openPeerRoster = oldOpenPeerRoster
		openPeerMailbox = oldOpenPeerMailbox
	})
}

func restoreCrawlBrokerSeam(t *testing.T) {
	t.Helper()
	oldOpenCrawlBroker := openCrawlBroker
	t.Cleanup(func() { openCrawlBroker = oldOpenCrawlBroker })
}

func restoreEvictionTickerSeam(t *testing.T) {
	t.Helper()
	oldNewEvictionTicks := newEvictionTicks
	t.Cleanup(func() { newEvictionTicks = oldNewEvictionTicks })
}

func restoreDHTOutboundTickerSeam(t *testing.T) {
	t.Helper()
	oldNewDHTOutboundTicks := newDHTOutboundTicks
	t.Cleanup(func() { newDHTOutboundTicks = oldNewDHTOutboundTicks })
}

func setValidRunEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		envLogLevel,
		envPeerAddr,
		envOpsAddr,
		envAdvertiseHost,
		envAdvertisePort,
		envStorageQuota,
		envTrustedProxies,
		envSeedlistURLs,
		envAnnounceInterval,
		envGreetsPerCycle,
		envNATSURL,
		envNATSOrdersSubject,
		envNATSIngestSubject,
		envNATSIngestDurable,
		envNATSIngestMaxMsgs,
		envNetworkDHT,
		envDHTDistribution,
		envDHTAllowWhileCrawling,
		envDHTAllowWhileIndexing,
		envDHTDistributionInterval,
	} {
		t.Setenv(key, "")
	}
	t.Setenv(envPeerHash, "0123456789AB")
	t.Setenv(envPeerName, "node")
	t.Setenv(envProxyURL, "http://proxy:4750")
	t.Setenv(envDataDir, t.TempDir())
}

func TestMainUsesRunNodeAndExitProcess(t *testing.T) {
	restoreMainSeams(t)
	runNode = func() error { return nil }
	exitProcess = func(code int) { t.Fatalf("exit code = %d, want no exit", code) }
	Main()

	restoreMainSeams(t)
	runNode = func() error { return errors.New("boom") }
	exitProcess = func(code int) { panic(exitStatus(code)) }
	defer func() {
		if recovered := recover(); recovered != exitStatus(1) {
			t.Fatalf("recovered = %v, want exit status 1", recovered)
		}
	}()
	Main()
}

func TestRunReturnsStageErrors(t *testing.T) {
	t.Run("logging", func(t *testing.T) {
		restoreMainSeams(t)
		t.Setenv(envLogLevel, "invalid")
		if err := run(); err == nil {
			t.Fatal("expected logging error")
		}
	})

	t.Run("crawl config", func(t *testing.T) {
		restoreMainSeams(t)
		setValidRunEnv(t)
		t.Setenv(envNATSURL, "nats://127.0.0.1:4222")
		t.Setenv(envNATSIngestMaxMsgs, "many")
		if err := run(); err == nil {
			t.Fatal("expected crawl config error")
		}
	})

	t.Run("open storage", func(t *testing.T) {
		restoreMainSeams(t)
		setValidRunEnv(t)
		sentinel := errors.New("open failed")
		openRuntimeVault = func(string, int64) (*vault.Vault, error) { return nil, sentinel }
		if err := run(); !errors.Is(err, sentinel) {
			t.Fatalf("run error = %v, want %v", err, sentinel)
		}
	})

	t.Run("assemble", func(t *testing.T) {
		restoreMainSeams(t)
		setValidRunEnv(t)
		sentinel := errors.New("assemble failed")
		openRuntimeVault = func(string, int64) (*vault.Vault, error) { return openTestVault(t), nil }
		assembleRuntimeNode = func(
			context.Context,
			nodeConfig,
			*vault.Vault,
			*http.Client,
			nodeTelemetry,
		) (node, error) {
			return node{}, sentinel
		}
		if err := run(); !errors.Is(err, sentinel) {
			t.Fatalf("run error = %v, want %v", err, sentinel)
		}
	})
}

func TestRunMountsCrawlDispatchAndServes(t *testing.T) {
	restoreMainSeams(t)
	setValidRunEnv(t)
	crawl := &recordingCrawl{}
	openRuntimeVault = func(string, int64) (*vault.Vault, error) { return openTestVault(t), nil }
	assembleRuntimeNode = func(
		context.Context,
		nodeConfig,
		*vault.Vault,
		*http.Client,
		nodeTelemetry,
	) (node, error) {
		return node{announcer: fakeAnnouncer{}, sweeper: &scriptedSweeper{}, crawl: crawl}, nil
	}
	serveRuntimeNode = func(
		_ context.Context,
		_ node,
		_ *metrics.EvictionMetrics,
		servers ...namedServer,
	) error {
		if len(servers) != 2 {
			t.Fatalf("servers = %d, want 2", len(servers))
		}

		return nil
	}

	if err := run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !crawl.mounted.Load() {
		t.Fatal("crawl dispatch was not mounted")
	}
}

func TestServeHandlesClosedServerAndCrawlRuntime(t *testing.T) {
	t.Run("closed server", func(t *testing.T) {
		restoreMainSeams(t)
		listenAndServeHTTP = func(*http.Server) error { return http.ErrServerClosed }
		err := serve(
			context.Background(),
			node{announcer: fakeAnnouncer{}, sweeper: &scriptedSweeper{}},
			metrics.NewEvictionMetrics(prometheus.NewRegistry()),
			namedServer{"closed", buildServer("127.0.0.1:0", http.NewServeMux())},
		)
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	})

	t.Run("crawl runtime", func(t *testing.T) {
		crawl := &recordingCrawl{}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := serve(
			ctx,
			node{announcer: fakeAnnouncer{}, sweeper: &scriptedSweeper{}, crawl: crawl},
			metrics.NewEvictionMetrics(prometheus.NewRegistry()),
		)
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
		if !crawl.ran.Load() || !crawl.closed.Load() {
			t.Fatalf(
				"crawl ran=%v closed=%v, want true/true",
				crawl.ran.Load(),
				crawl.closed.Load(),
			)
		}
	})
}

func TestShutdownReturnsServerErrors(t *testing.T) {
	restoreMainSeams(t)
	sentinel := errors.New("shutdown failed")
	shutdownHTTPServer = func(*http.Server, context.Context) error { return sentinel }
	err := shutdown([]namedServer{{
		name:   "ops",
		server: buildServer("127.0.0.1:0", http.NewServeMux()),
	}})
	if !errors.Is(err, sentinel) {
		t.Fatalf("shutdown error = %v, want %v", err, sentinel)
	}
}

func TestCloseVaultLogsCloseFailure(t *testing.T) {
	t.Helper()
	closeVault(failingCloser{})
}

func TestEvictionLoopAndSweepBranches(t *testing.T) {
	t.Run("ticker", func(t *testing.T) {
		restoreEvictionTickerSeam(t)
		ticks := make(chan time.Time, 1)
		var stopped atomic.Bool
		newEvictionTicks = func(time.Duration) (<-chan time.Time, func()) {
			return ticks, func() { stopped.Store(true) }
		}
		ctx, cancel := context.WithCancel(context.Background())
		sweeper := &scriptedSweeper{
			called:   make(chan int32, 2),
			cancel:   cancel,
			cancelOn: 2,
		}
		done := make(chan struct{})
		go func() {
			runEvictionLoop(ctx, sweeper, metrics.NewEvictionMetrics(prometheus.NewRegistry()))
			close(done)
		}()
		if call := <-sweeper.called; call != 1 {
			t.Fatalf("first call = %d, want 1", call)
		}
		ticks <- time.Now()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("eviction loop did not stop")
		}
		if sweeper.calls.Load() != 2 || !stopped.Load() {
			t.Fatalf("calls=%d stopped=%v, want 2/true", sweeper.calls.Load(), stopped.Load())
		}
	})

	t.Run("failure", func(t *testing.T) {
		sweepOnce(
			context.Background(),
			&scriptedSweeper{err: errors.New("sweep failed")},
			metrics.NewEvictionMetrics(prometheus.NewRegistry()),
		)
	})

	t.Run("deleted", func(t *testing.T) {
		sweepOnce(
			context.Background(),
			&scriptedSweeper{result: eviction.Result{URLsDeleted: 1, PostingsDeleted: 2}},
			metrics.NewEvictionMetrics(prometheus.NewRegistry()),
		)
	})
}

func TestDHTOutboundLoopAndCycleBranches(t *testing.T) {
	t.Run("ticker", func(t *testing.T) {
		restoreDHTOutboundTickerSeam(t)
		ticks := make(chan time.Time, 1)
		var stopped atomic.Bool
		newDHTOutboundTicks = func(time.Duration) (<-chan time.Time, func()) {
			return ticks, func() { stopped.Store(true) }
		}
		ctx, cancel := context.WithCancel(context.Background())
		cycle := &scriptedDHTOutboundCycle{
			called:   make(chan int32, 2),
			cancel:   cancel,
			cancelOn: 2,
		}
		done := make(chan struct{})
		go func() {
			runDHTOutboundLoop(
				ctx,
				dhtOutboundProcess{cycle: cycle, interval: defaultDHTDistributionInterval},
			)
			close(done)
		}()
		if call := <-cycle.called; call != 1 {
			t.Fatalf("first call = %d, want 1", call)
		}
		ticks <- time.Now()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("dht outbound loop did not stop")
		}
		if cycle.calls.Load() != 2 || !stopped.Load() {
			t.Fatalf("calls=%d stopped=%v, want 2/true", cycle.calls.Load(), stopped.Load())
		}
	})

	t.Run("failure", func(t *testing.T) {
		runDHTOutboundOnce(
			context.Background(),
			&scriptedDHTOutboundCycle{
				receipt: dhtexchange.ScheduledDistributionReceipt{
					Distribution: dhtexchange.DistributionReceipt{
						State: dhtexchange.DistributionCapacityFailed,
						Peer:  yacymodel.Hash("AAAAAAAAAAAA"),
					},
				},
				err: errors.New("dht failed"),
			},
		)
	})

	t.Run("sent", func(t *testing.T) {
		runDHTOutboundOnce(
			context.Background(),
			&scriptedDHTOutboundCycle{
				receipt: dhtexchange.ScheduledDistributionReceipt{
					Distribution: dhtexchange.DistributionReceipt{
						State:        dhtexchange.DistributionSent,
						Peer:         yacymodel.Hash("BBBBBBBBBBBB"),
						PostingCount: 2,
					},
				},
			},
		)
	})
}

func TestDHTGateStateSnapshot(t *testing.T) {
	reachability := &publicReachabilityScript{reachable: true}
	source := dhtGateStateSource{
		reachability: reachability,
		storage:      capacityProbe{},
		postings:     rwiCounter{count: 123},
		roster: reachableRoster{peers: []yacymodel.Seed{
			{Hash: yacymodel.Hash("AAAAAAAAAAAA")},
			{Hash: yacymodel.Hash("BBBBBBBBBBBB")},
		}},
	}

	state := source.Snapshot(context.Background())
	if !state.PublicReachable ||
		!state.LocalPeerKnown ||
		state.LocalPeerVirgin ||
		state.ConnectedPeers != 2 ||
		state.LocalRWIWords != 123 ||
		!state.StorageAvailable {
		t.Fatalf("state = %#v", state)
	}
	if reachability.calls.Load() != 1 {
		t.Fatalf("reachability calls = %d, want 1", reachability.calls.Load())
	}

	source.storage = capacityProbe{atCapacity: true}
	source.postings = rwiCounter{err: errors.New("count failed")}
	state = source.Snapshot(context.Background())
	if state.LocalRWIWords != 0 || state.StorageAvailable {
		t.Fatalf("at capacity state = %#v", state)
	}

	source.storage = capacityProbe{err: errors.New("capacity failed")}
	state = source.Snapshot(context.Background())
	if state.StorageAvailable {
		t.Fatalf("storage error state = %#v", state)
	}

	source.reachability = nil
	state = source.Snapshot(context.Background())
	if state.PublicReachable {
		t.Fatalf("nil reachability state = %#v", state)
	}
}

func TestOpenNodeStorageReturnsOpenErrors(t *testing.T) {
	sentinel := errors.New("open failed")

	t.Run("staleness", func(t *testing.T) {
		restoreStorageSeams(t)
		openStalenessRanking = func(*vault.Vault) (urlmetastaleness.StalenessRanking, error) {
			return nil, sentinel
		}
		if _, err := openNodeStorage(openTestVault(t), ""); !errors.Is(err, sentinel) {
			t.Fatalf("open error = %v, want %v", err, sentinel)
		}
	})

	t.Run("urlmeta", func(t *testing.T) {
		restoreStorageSeams(t)
		openURLMetadata = func(
			*vault.Vault,
			...urlmeta.URLMetadataObserver,
		) (urlmeta.URLDirectory, urlmeta.URLEvictor, urlmeta.URLReceiver, error) {
			return nil, nil, nil, sentinel
		}
		if _, err := openNodeStorage(openTestVault(t), ""); !errors.Is(err, sentinel) {
			t.Fatalf("open error = %v, want %v", err, sentinel)
		}
	})

	t.Run("references", func(t *testing.T) {
		restoreStorageSeams(t)
		openURLReferences = func(*vault.Vault) (urlreferences.ReferenceProjection, error) {
			return nil, sentinel
		}
		if _, err := openNodeStorage(openTestVault(t), ""); !errors.Is(err, sentinel) {
			t.Fatalf("open error = %v, want %v", err, sentinel)
		}
	})

	t.Run("rwi", func(t *testing.T) {
		restoreStorageSeams(t)
		openRWIStorage = func(
			*vault.Vault,
			urlmeta.URLDirectory,
			rwi.Config,
			...rwi.PostingObserver,
		) (rwi.PostingIndex, rwi.PostingReceiver, rwi.PostingPurger, error) {
			return nil, nil, nil, sentinel
		}
		if _, err := openNodeStorage(openTestVault(t), ""); !errors.Is(err, sentinel) {
			t.Fatalf("open error = %v, want %v", err, sentinel)
		}
	})

	t.Run("rwi outbound", func(t *testing.T) {
		restoreStorageSeams(t)
		openRWIStorage = func(
			*vault.Vault,
			urlmeta.URLDirectory,
			rwi.Config,
			...rwi.PostingObserver,
		) (rwi.PostingIndex, rwi.PostingReceiver, rwi.PostingPurger, error) {
			return postingIndexOnly{}, nil, nil, nil
		}
		if _, err := openNodeStorage(openTestVault(t), ""); err == nil {
			t.Fatal("expected outbound rwi storage error")
		}
	})

	t.Run("rwi outbound recovery", func(t *testing.T) {
		restoreStorageSeams(t)
		openRWIStorage = func(
			*vault.Vault,
			urlmeta.URLDirectory,
			rwi.Config,
			...rwi.PostingObserver,
		) (rwi.PostingIndex, rwi.PostingReceiver, rwi.PostingPurger, error) {
			return &outboundPostingStoreScript{recoverErr: sentinel}, nil, nil, nil
		}
		if _, err := openNodeStorage(openTestVault(t), ""); !errors.Is(err, sentinel) {
			t.Fatalf("open error = %v, want %v", err, sentinel)
		}
	})
}

func TestOpenNodeStorageReturnsSearchIndexOpenError(t *testing.T) {
	sentinel := errors.New("open failed")
	restoreStorageSeams(t)
	openSearchIndex = func(
		context.Context,
		string,
		documentstore.DocumentDirectory,
	) (searchindex.SearchIndex, error) {
		return nil, sentinel
	}
	if _, err := openNodeStorage(openTestVault(t), ""); !errors.Is(err, sentinel) {
		t.Fatalf("open error = %v, want %v", err, sentinel)
	}
}

func TestOpenNodeStorageUsesConfiguredSearchIndexPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), searchIndexDirName)
	storage, err := openNodeStorage(openTestVault(t), path)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	if closer, ok := storage.searchIndex.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	stats, err := storage.searchIndex.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Backend != "bleve-disk" {
		t.Fatalf("backend = %q, want bleve-disk", stats.Backend)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat search index path: %v", err)
	}
}

func TestOpenNodeStorageReturnsDocumentOpenError(t *testing.T) {
	sentinel := errors.New("open failed")
	restoreStorageSeams(t)
	openDocuments = func(*vault.Vault) (
		documentstore.DocumentDirectory,
		documentstore.DocumentReceiver,
		error,
	) {
		return nil, nil, sentinel
	}
	if _, err := openNodeStorage(openTestVault(t), ""); !errors.Is(err, sentinel) {
		t.Fatalf("open error = %v, want %v", err, sentinel)
	}
}

func TestAssembleNodeReturnsSetupErrors(t *testing.T) {
	sentinel := errors.New("setup failed")

	t.Run("storage", func(t *testing.T) {
		restoreAssemblySeams(t)
		openRuntimeNodeStorage = func(*vault.Vault, string) (nodeStorage, error) {
			return nodeStorage{}, sentinel
		}
		_, err := assembleNode(
			context.Background(),
			testConfig(t),
			openTestVault(t),
			http.DefaultClient,
			nodeTelemetry{
				dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
				dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
			},
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("assemble error = %v, want %v", err, sentinel)
		}
	})

	t.Run("peer exchange", func(t *testing.T) {
		restoreAssemblySeams(t)
		assembleRuntimePeerExchange = func(peerExchange) (peerExchangeRuntime, error) {
			return peerExchangeRuntime{}, sentinel
		}
		_, err := assembleNode(
			context.Background(),
			testConfig(t),
			openTestVault(t),
			http.DefaultClient,
			nodeTelemetry{
				dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
				dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
				peer:        metrics.NewPeerMetrics(prometheus.NewRegistry()),
			},
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("assemble error = %v, want %v", err, sentinel)
		}
	})

	t.Run("crawl", func(t *testing.T) {
		restoreAssemblySeams(t)
		assembleRuntimePeerExchange = func(peerExchange) (peerExchangeRuntime, error) {
			return peerExchangeRuntime{announcer: fakeAnnouncer{}}, nil
		}
		buildRuntimeCrawl = func(
			context.Context,
			crawlConfig,
			nodeidentity.Identity,
			nodeStorage,
		) (crawlProcess, error) {
			return nil, sentinel
		}
		_, err := assembleNode(
			context.Background(),
			testConfig(t),
			openTestVault(t),
			http.DefaultClient,
			nodeTelemetry{
				dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
				dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
			},
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("assemble error = %v, want %v", err, sentinel)
		}
	})
}

func TestAssembleNodeReturnsPeerBirthDateError(t *testing.T) {
	sentinel := errors.New("birth date failed")
	restoreAssemblySeams(t)
	openRuntimePeerBirthDate = func(context.Context, *vault.Vault, func() time.Time) (time.Time, error) {
		return time.Time{}, sentinel
	}
	_, err := assembleNode(
		context.Background(),
		testConfig(t),
		openTestVault(t),
		http.DefaultClient,
		nodeTelemetry{
			dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
			dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("assemble error = %v, want %v", err, sentinel)
	}
}

func TestAssembleNodeReturnsPeerNewsError(t *testing.T) {
	sentinel := errors.New("news failed")
	restoreAssemblySeams(t)
	openRuntimePeerNews = func(*vault.Vault, func() time.Time) (*peernews.Pool, error) {
		return nil, sentinel
	}
	_, err := assembleNode(
		context.Background(),
		testConfig(t),
		openTestVault(t),
		http.DefaultClient,
		nodeTelemetry{
			dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
			dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("assemble error = %v, want %v", err, sentinel)
	}
}

func TestAssembleNodeReturnsTransferTallyError(t *testing.T) {
	sentinel := errors.New("tally failed")
	restoreAssemblySeams(t)
	openRuntimeTransferTally = func(*vault.Vault) (*transfertally.Tally, error) {
		return nil, sentinel
	}
	_, err := assembleNode(
		context.Background(),
		testConfig(t),
		openTestVault(t),
		http.DefaultClient,
		nodeTelemetry{
			dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
			dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("assemble error = %v, want %v", err, sentinel)
	}
}

func TestPeerExchangeReturnsOpenErrors(t *testing.T) {
	sentinel := errors.New("peer exchange failed")

	t.Run("roster", func(t *testing.T) {
		restorePeerExchangeSeams(t)
		openPeerRoster = func(*vault.Vault, func() time.Time, int, int) (peerroster.Roster, error) {
			return nil, sentinel
		}
		_, err := assembleNode(
			context.Background(),
			testConfig(t),
			openTestVault(t),
			http.DefaultClient,
			nodeTelemetry{
				dhtOutbound: metrics.NewDHTOutboundMetrics(prometheus.NewRegistry()),
				dhtInbound:  metrics.NewDHTInboundMetrics(prometheus.NewRegistry()),
			},
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("assemble error = %v, want %v", err, sentinel)
		}
	})

	t.Run("mailbox", func(t *testing.T) {
		restorePeerExchangeSeams(t)
		openPeerMailbox = func(*vault.Vault, func() time.Time) (*peermessage.Mailbox, error) {
			return nil, sentinel
		}
		_, err := (peerExchange{
			vault:  openTestVault(t),
			peer:   metrics.NewPeerMetrics(prometheus.NewRegistry()),
			roster: fakeRoster{},
		}).assemble()
		if !errors.Is(err, sentinel) {
			t.Fatalf("assemble error = %v, want %v", err, sentinel)
		}
	})
}

func TestBuildCrawlRuntimeReturnsBrokerError(t *testing.T) {
	restoreCrawlBrokerSeam(t)
	sentinel := errors.New("broker failed")
	openCrawlBroker = func(context.Context, crawlbroker.Config) (*crawlbroker.CrawlBroker, error) {
		return nil, sentinel
	}
	_, err := buildCrawlRuntime(
		context.Background(),
		crawlConfig{NATSURL: "nats://127.0.0.1:4222"},
		nodeIdentity(testConfig(t)),
		nodeStorage{},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("build error = %v, want %v", err, sentinel)
	}
}

func TestMemVaultOpenRuntimeSeam(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open memvault: %v", err)
	}
	closeVault(v)
}
