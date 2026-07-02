package main

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/crawlbroker"
	"github.com/D4rk4/yago/yacynode/internal/eviction"
	"github.com/D4rk4/yago/yacynode/internal/memvault"
	"github.com/D4rk4/yago/yacynode/internal/metrics"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacynode/internal/peerannouncement"
	"github.com/D4rk4/yago/yacynode/internal/peermessage"
	"github.com/D4rk4/yago/yacynode/internal/peerroster"
	"github.com/D4rk4/yago/yacynode/internal/rwi"
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

func (fakeRoster) FreshestPeers(context.Context, int) []yacymodel.Seed { return nil }

func (fakeRoster) ReachablePeers(context.Context) []yacymodel.Seed { return nil }

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
	oldAssembleRuntimePeerExchange := assembleRuntimePeerExchange
	oldBuildRuntimeCrawl := buildRuntimeCrawl
	t.Cleanup(func() {
		openRuntimeNodeStorage = oldOpenRuntimeNodeStorage
		assembleRuntimePeerExchange = oldAssembleRuntimePeerExchange
		buildRuntimeCrawl = oldBuildRuntimeCrawl
	})
}

func restoreStorageSeams(t *testing.T) {
	t.Helper()
	oldOpenStalenessRanking := openStalenessRanking
	oldOpenURLMetadata := openURLMetadata
	oldOpenURLReferences := openURLReferences
	oldOpenRWIStorage := openRWIStorage
	t.Cleanup(func() {
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
	main()

	restoreMainSeams(t)
	runNode = func() error { return errors.New("boom") }
	exitProcess = func(code int) { panic(exitStatus(code)) }
	defer func() {
		if recovered := recover(); recovered != exitStatus(1) {
			t.Fatalf("recovered = %v, want exit status 1", recovered)
		}
	}()
	main()
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
		assembleRuntimeNode = func(context.Context, nodeConfig, *vault.Vault, *http.Client) (node, error) {
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
	assembleRuntimeNode = func(context.Context, nodeConfig, *vault.Vault, *http.Client) (node, error) {
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

func TestOpenNodeStorageReturnsOpenErrors(t *testing.T) {
	sentinel := errors.New("open failed")

	t.Run("staleness", func(t *testing.T) {
		restoreStorageSeams(t)
		openStalenessRanking = func(*vault.Vault) (urlmetastaleness.StalenessRanking, error) {
			return nil, sentinel
		}
		if _, err := openNodeStorage(openTestVault(t)); !errors.Is(err, sentinel) {
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
		if _, err := openNodeStorage(openTestVault(t)); !errors.Is(err, sentinel) {
			t.Fatalf("open error = %v, want %v", err, sentinel)
		}
	})

	t.Run("references", func(t *testing.T) {
		restoreStorageSeams(t)
		openURLReferences = func(*vault.Vault) (urlreferences.ReferenceProjection, error) {
			return nil, sentinel
		}
		if _, err := openNodeStorage(openTestVault(t)); !errors.Is(err, sentinel) {
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
		if _, err := openNodeStorage(openTestVault(t)); !errors.Is(err, sentinel) {
			t.Fatalf("open error = %v, want %v", err, sentinel)
		}
	})
}

func TestAssembleNodeReturnsSetupErrors(t *testing.T) {
	sentinel := errors.New("setup failed")

	t.Run("storage", func(t *testing.T) {
		restoreAssemblySeams(t)
		openRuntimeNodeStorage = func(*vault.Vault) (nodeStorage, error) {
			return nodeStorage{}, sentinel
		}
		_, err := assembleNode(
			context.Background(),
			testConfig(t),
			openTestVault(t),
			http.DefaultClient,
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("assemble error = %v, want %v", err, sentinel)
		}
	})

	t.Run("peer exchange", func(t *testing.T) {
		restoreAssemblySeams(t)
		assembleRuntimePeerExchange = func(peerExchange) (peerannouncement.Announcer, error) {
			return nil, sentinel
		}
		_, err := assembleNode(
			context.Background(),
			testConfig(t),
			openTestVault(t),
			http.DefaultClient,
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("assemble error = %v, want %v", err, sentinel)
		}
	})

	t.Run("crawl", func(t *testing.T) {
		restoreAssemblySeams(t)
		assembleRuntimePeerExchange = func(peerExchange) (peerannouncement.Announcer, error) {
			return fakeAnnouncer{}, nil
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
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("assemble error = %v, want %v", err, sentinel)
		}
	})
}

func TestPeerExchangeReturnsOpenErrors(t *testing.T) {
	sentinel := errors.New("peer exchange failed")

	t.Run("roster", func(t *testing.T) {
		restorePeerExchangeSeams(t)
		openPeerRoster = func(*vault.Vault, func() time.Time, int, int) (peerroster.Roster, error) {
			return nil, sentinel
		}
		_, err := (peerExchange{vault: openTestVault(t)}).assemble()
		if !errors.Is(err, sentinel) {
			t.Fatalf("assemble error = %v, want %v", err, sentinel)
		}
	})

	t.Run("mailbox", func(t *testing.T) {
		restorePeerExchangeSeams(t)
		openPeerRoster = func(*vault.Vault, func() time.Time, int, int) (peerroster.Roster, error) {
			return fakeRoster{}, nil
		}
		openPeerMailbox = func(*vault.Vault, func() time.Time) (*peermessage.Mailbox, error) {
			return nil, sentinel
		}
		_, err := (peerExchange{vault: openTestVault(t)}).assemble()
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
