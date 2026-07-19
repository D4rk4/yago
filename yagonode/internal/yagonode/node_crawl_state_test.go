package yagonode

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/shardvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func restoreCrawlRuntimeStateSeams(t *testing.T) {
	t.Helper()
	oldAcquire := acquireCrawlRuntimeStateStartupLease
	oldOpen := openCrawlRuntimeStateVault
	oldMigrateBroker := migrateCrawlBrokerState
	oldMigrateRuns := migrateCrawlRunState
	oldClose := closeCrawlRuntimeStateVault
	t.Cleanup(func() {
		acquireCrawlRuntimeStateStartupLease = oldAcquire
		openCrawlRuntimeStateVault = oldOpen
		migrateCrawlBrokerState = oldMigrateBroker
		migrateCrawlRunState = oldMigrateRuns
		closeCrawlRuntimeStateVault = oldClose
	})
}

func closeCrawlRuntimeStateForTest(t *testing.T, state *vault.Vault, owned bool) {
	t.Helper()
	if err := closeOwnedCrawlRuntimeState(state, owned); err != nil {
		t.Errorf("close dedicated crawl state: %v", err)
	}
}

func TestOpenCrawlRuntimeStateMigratesBrokerAndRunState(t *testing.T) {
	legacy, err := shardvault.Open(filepath.Join(t.TempDir(), "legacy"), 0)
	if err != nil {
		t.Fatalf("open legacy state: %v", err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	legacyBroker, err := crawlbroker.Open(
		crawlbroker.Config{ListenAddr: "127.0.0.1:0"},
		legacy,
		nil,
	)
	if err != nil {
		t.Fatalf("open legacy broker: %v", err)
	}
	t.Cleanup(legacyBroker.Close)
	order := crawlStateTestOrder("retained")
	duplicate, err := legacyBroker.Orders.PublishOnce(t.Context(), "retained-key", order)
	if err != nil || duplicate {
		t.Fatalf("publish legacy order: duplicate=%t error=%v", duplicate, err)
	}
	legacyRuns, err := crawlruns.Open(t.Context(), legacy, 4)
	if err != nil {
		t.Fatalf("open legacy runs: %v", err)
	}
	identity := make([]byte, 32)
	identity[0] = 1
	progress := yagocrawlcontract.CrawlRunProgress{
		RunID:         "retained-run",
		WorkerID:      "worker",
		ProfileHandle: "profile",
		ProfileName:   "retained",
		State:         yagocrawlcontract.CrawlRunFinished,
		Tally:         yagocrawlcontract.CrawlRunTally{Fetched: 1, Indexed: 1},
	}
	if err := legacyRuns.RecordTerminal(t.Context(), identity, progress); err != nil {
		t.Fatalf("record legacy terminal run: %v", err)
	}

	state, owned, err := openCrawlRuntimeState(
		t.Context(),
		filepath.Join(t.TempDir(), crawlBrokerStateFileName),
		legacy,
	)
	if err != nil {
		t.Fatalf("open dedicated crawl state: %v", err)
	}
	if !owned || state == legacy {
		t.Fatalf("dedicated crawl state ownership = %t state=%p legacy=%p", owned, state, legacy)
	}
	t.Cleanup(func() { closeCrawlRuntimeStateForTest(t, state, owned) })
	dedicatedRuns, err := crawlruns.Open(t.Context(), state, 4)
	if err != nil {
		t.Fatalf("open dedicated runs: %v", err)
	}
	dedicatedBroker, err := crawlbroker.Open(
		crawlbroker.Config{ListenAddr: "127.0.0.1:0"},
		state,
		dedicatedRuns,
	)
	if err != nil {
		t.Fatalf("open dedicated broker: %v", err)
	}
	t.Cleanup(dedicatedBroker.Close)
	depth, err := dedicatedBroker.Orders.Depth(t.Context())
	if err != nil || depth.Pending != 1 || depth.Leased != 0 {
		t.Fatalf("dedicated queue depth = %+v, error=%v", depth, err)
	}
	duplicate, err = dedicatedBroker.Orders.PublishOnce(t.Context(), "retained-key", order)
	if err != nil || !duplicate {
		t.Fatalf("migrated idempotency: duplicate=%t error=%v", duplicate, err)
	}
	runs := dedicatedRuns.Recent()
	if len(runs) != 1 || runs[0].RunID != progress.RunID || runs[0].Tally.Fetched != 1 {
		t.Fatalf("migrated terminal runs = %+v", runs)
	}
	legacyDepth, err := legacyBroker.Orders.Depth(t.Context())
	if err != nil || legacyDepth.Pending != 1 {
		t.Fatalf("legacy source changed: depth=%+v error=%v", legacyDepth, err)
	}
}

func TestCrawlRuntimeClosesOwnedState(t *testing.T) {
	storageVault, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	t.Cleanup(func() { _ = storageVault.Close() })
	storage, err := openNodeStorage(storageVault, "")
	if err != nil {
		t.Fatalf("open node components: %v", err)
	}
	path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)
	runtime, err := buildCrawlRuntime(
		t.Context(),
		crawlConfig{ListenAddr: "127.0.0.1:0", StatePath: path},
		nodeIdentity(testConfig(t)),
		storage,
		storageVault,
	)
	if err != nil {
		t.Fatalf("build crawl runtime: %v", err)
	}
	runtime.Close()
	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen closed crawl state: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened crawl state: %v", err)
	}
}

func TestCrawlRuntimeClosesOwnedStateAfterBrokerFailure(t *testing.T) {
	restoreCrawlBrokerSeam(t)
	sentinel := errors.New("broker failed")
	openCrawlBroker = func(
		crawlbroker.Config,
		*vault.Vault,
		crawlbroker.ProgressSink,
	) (*crawlbroker.CrawlBroker, error) {
		return nil, sentinel
	}
	storageVault, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open node storage: %v", err)
	}
	t.Cleanup(func() { _ = storageVault.Close() })
	path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)
	_, err = buildCrawlRuntime(
		t.Context(),
		crawlConfig{ListenAddr: "127.0.0.1:0", StatePath: path},
		nodeIdentity(testConfig(t)),
		nodeStorage{},
		storageVault,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("build crawl runtime error = %v, want %v", err, sentinel)
	}
	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen crawl state after error: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened crawl state: %v", err)
	}
}

func TestOpenCrawlRuntimeStateReportsStorageOpenFailure(t *testing.T) {
	restoreCrawlRuntimeStateSeams(t)
	want := errors.New("state open failed")
	openCrawlRuntimeStateVault = func(string) (*vault.Vault, error) {
		return nil, want
	}

	state, owned, err := openCrawlRuntimeState(
		t.Context(),
		filepath.Join(t.TempDir(), crawlBrokerStateFileName),
		openTestVault(t),
	)
	if state != nil || owned || !errors.Is(err, want) ||
		!strings.Contains(err.Error(), "open crawl runtime state") {
		t.Fatalf("open result = state=%p owned=%t error=%v", state, owned, err)
	}
}

func TestCrawlRuntimeStateProvisioningHonorsStoragePressure(t *testing.T) {
	tests := []struct {
		name       string
		createFile bool
	}{
		{name: "missing"},
		{name: "zero length", createFile: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertCrawlRuntimeStateProvisioningPressure(t, test.createFile)
		})
	}
}

func TestCrawlRuntimeStateProvisioningReportsInspectionFailure(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "state-loop")
	if err := os.Symlink("state-loop", path); err != nil {
		t.Fatalf("create state symlink loop: %v", err)
	}
	if _, err := openCrawlRuntimeStateStorage(path, nil); err == nil ||
		!strings.Contains(err.Error(), "inspect crawl runtime state") {
		t.Fatalf("state inspection error = %v", err)
	}
}

func assertCrawlRuntimeStateProvisioningPressure(t *testing.T, createFile bool) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, crawlBrokerStateFileName)
	if createFile {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("create empty crawl state: %v", err)
		}
	}
	pressure := yagocrawlcontract.NewStoragePressureGate(
		root,
		yagocrawlcontract.StoragePressurePolicy{ReservedFreeBytes: math.MaxUint64},
	)

	state, owned, err := openCrawlRuntimeState(
		t.Context(),
		path,
		openTestVault(t),
		pressure,
	)
	if state != nil || owned || !errors.Is(err, yagocrawlcontract.ErrStoragePressure) {
		t.Fatalf("pressure result = state=%p owned=%t error=%v", state, owned, err)
	}
	info, statErr := os.Stat(path)
	if createFile {
		if statErr != nil || info.Size() != 0 {
			t.Fatalf("empty state after rejection = info=%v error=%v", info, statErr)
		}

		return
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("missing state after rejection error = %v", statErr)
	}
}

func TestExistingCrawlRuntimeStateOpensUnderStoragePressure(t *testing.T) {
	restoreCrawlRuntimeStateSeams(t)
	root := t.TempDir()
	path := filepath.Join(root, crawlBrokerStateFileName)
	existing, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("create existing crawl state: %v", err)
	}
	if err := existing.Close(); err != nil {
		t.Fatalf("close existing crawl state: %v", err)
	}
	migrateCrawlBrokerState = func(
		context.Context,
		*vault.Vault,
		*vault.Vault,
		vault.RetainedBucketMigrationAdmission,
	) error {
		return nil
	}
	migrateCrawlRunState = migrateCrawlBrokerState
	pressure := yagocrawlcontract.NewStoragePressureGate(
		root,
		yagocrawlcontract.StoragePressurePolicy{ReservedFreeBytes: math.MaxUint64},
	)

	state, owned, err := openCrawlRuntimeState(
		t.Context(),
		path,
		openTestVault(t),
		pressure,
	)
	if err != nil || state == nil || !owned {
		t.Fatalf("existing state under pressure = state=%p owned=%t error=%v", state, owned, err)
	}
	closeCrawlRuntimeStateForTest(t, state, owned)
}

func TestOpenCrawlRuntimeStateClosesDedicatedStorageAfterMigrationFailure(t *testing.T) {
	tests := []struct {
		name      string
		configure func(error)
		context   string
	}{
		{
			name: "broker",
			configure: func(want error) {
				migrateCrawlBrokerState = func(
					context.Context,
					*vault.Vault,
					*vault.Vault,
					vault.RetainedBucketMigrationAdmission,
				) error {
					return want
				}
			},
			context: "migrate crawl broker state",
		},
		{
			name: "runs",
			configure: func(want error) {
				migrateCrawlRunState = func(
					context.Context,
					*vault.Vault,
					*vault.Vault,
					vault.RetainedBucketMigrationAdmission,
				) error {
					return want
				}
			},
			context: "migrate crawl run state",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restoreCrawlRuntimeStateSeams(t)
			want := errors.New(test.name + " migration failed")
			test.configure(want)
			path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)

			state, owned, err := openCrawlRuntimeState(t.Context(), path, openTestVault(t))
			if state != nil || owned || !errors.Is(err, want) ||
				!strings.Contains(err.Error(), test.context) {
				t.Fatalf("migration result = state=%p owned=%t error=%v", state, owned, err)
			}
			reopened, err := boltvault.Open(path, 0)
			if err != nil {
				t.Fatalf("reopen state after migration failure: %v", err)
			}
			if err := reopened.Close(); err != nil {
				t.Fatalf("close reopened state: %v", err)
			}
		})
	}
}

func TestCrawlRuntimeStateFailurePreservesCleanupFailure(t *testing.T) {
	restoreCrawlRuntimeStateSeams(t)
	state := openTestVault(t)
	wantFailure := errors.New("migration failed")
	wantClose := errors.New("state close failed")
	closeCrawlRuntimeStateVault = func(*vault.Vault) error { return wantClose }

	err := crawlRuntimeStateFailure(wantFailure, state, true)
	if !errors.Is(err, wantFailure) || !errors.Is(err, wantClose) ||
		!strings.Contains(err.Error(), "close crawl runtime state") {
		t.Fatalf("combined failure = %v", err)
	}
}

func TestBuildCrawlRuntimeReportsDedicatedStateOpenFailure(t *testing.T) {
	restoreCrawlRuntimeStateSeams(t)
	want := errors.New("state open failed")
	openCrawlRuntimeStateVault = func(string) (*vault.Vault, error) {
		return nil, want
	}

	_, err := buildCrawlRuntime(
		t.Context(),
		crawlConfig{
			ListenAddr: "127.0.0.1:0",
			StatePath:  filepath.Join(t.TempDir(), crawlBrokerStateFileName),
		},
		nodeIdentity(testConfig(t)),
		nodeStorage{},
		openTestVault(t),
	)
	if !errors.Is(err, want) {
		t.Fatalf("build crawl runtime error = %v, want %v", err, want)
	}
}

func TestCrawlRuntimeReportsOwnedStateCloseFailure(t *testing.T) {
	restoreCrawlRuntimeStateSeams(t)
	storageVault := openTestVault(t)
	storage, err := openNodeStorage(storageVault, "")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	runtime, err := buildCrawlRuntime(
		t.Context(),
		crawlConfig{
			ListenAddr: "127.0.0.1:0",
			StatePath:  filepath.Join(t.TempDir(), crawlBrokerStateFileName),
		},
		nodeIdentity(testConfig(t)),
		storage,
		storageVault,
	)
	if err != nil {
		t.Fatalf("build crawl runtime: %v", err)
	}
	want := errors.New("state close failed")
	closeCrawlRuntimeStateVault = func(*vault.Vault) error { return want }
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	runtime.Close()
	if err := runtime.state.Close(); err != nil {
		t.Fatalf("close state after injected failure: %v", err)
	}
	if logOutput := output.String(); !strings.Contains(
		logOutput,
		msgCrawlRuntimeStateCloseFailed,
	) ||
		!strings.Contains(logOutput, want.Error()) {
		t.Fatalf("close failure log = %q", logOutput)
	}
}

func TestOpenCrawlRuntimeStateRequiresLegacyStorage(t *testing.T) {
	state, owned, err := openCrawlRuntimeState(context.Background(), "", nil)
	if err != nil || state != nil || owned {
		t.Fatalf("empty path fallback = state=%p owned=%t error=%v", state, owned, err)
	}
	_, _, err = openCrawlRuntimeState(
		context.Background(),
		filepath.Join(t.TempDir(), crawlBrokerStateFileName),
		nil,
	)
	if err == nil {
		t.Fatal("dedicated state accepted missing legacy storage")
	}
}

func crawlStateTestOrder(name string) yagocrawlcontract.CrawlOrder {
	return yagocrawlcontract.CrawlOrder{
		Provenance: []byte("admin"),
		Profile: yagocrawlcontract.NewCrawlProfile(
			yagocrawlcontract.CrawlProfile{Name: name},
		),
		Requests: []yagocrawlcontract.CrawlRequest{{URL: "https://example.org/" + name}},
	}
}
