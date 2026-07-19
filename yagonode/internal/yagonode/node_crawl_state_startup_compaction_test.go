package yagonode

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestCompactCrawlRuntimeStateCopiesLiveRowsAndSecuresMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)
	bloatCrawlRuntimeState(t, path)
	sourceInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inspect crawl state mode: %v", err)
	}
	if err := os.Chmod(path, sourceInfo.Mode().Perm()|0o040); err != nil {
		t.Fatalf("set crawl state mode: %v", err)
	}
	before := crawlStateFileSize(t, path)
	temporaryPath := path + crawlStateCompactionSuffix
	if err := os.WriteFile(temporaryPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale crawl state compaction file: %v", err)
	}

	result, err := compactCrawlRuntimeState(path, before-1)
	if err != nil {
		t.Fatalf("compact crawl state: %v", err)
	}
	if !result.installed || result.afterBytes >= result.beforeBytes {
		t.Fatalf("crawl state compaction = %+v", result)
	}
	if _, err := os.Stat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale crawl state compaction file remains: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat compacted crawl state: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("compacted crawl state mode = %o, want 600", info.Mode().Perm())
	}
	if value := readRetainedCrawlRuntimeState(t, path); !bytes.Equal(value, []byte("live")) {
		t.Fatalf("retained value = %q", value)
	}
}

func TestCompactCrawlRuntimeStateCompactsFileAtMaximum(t *testing.T) {
	path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)
	bloatCrawlRuntimeState(t, path)
	before := crawlStateFileSize(t, path)
	result, err := compactCrawlRuntimeState(path, before)
	if err != nil {
		t.Fatalf("inspect crawl state at maximum: %v", err)
	}
	if !result.installed || crawlStateFileSize(t, path) >= before {
		t.Fatalf("crawl state was not compacted at maximum: %+v", result)
	}
}

func TestCompactCrawlRuntimeStateSkipsFileBelowMaximum(t *testing.T) {
	path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)
	bloatCrawlRuntimeState(t, path)
	before := crawlStateFileSize(t, path)
	result, err := compactCrawlRuntimeState(path, before+1)
	if err != nil {
		t.Fatalf("inspect crawl state below maximum: %v", err)
	}
	if result.installed || crawlStateFileSize(t, path) != before {
		t.Fatalf("crawl state compacted below maximum: %+v", result)
	}
}

type crawlStateCompactionAdmissionProbe struct {
	failure    error
	measured   uint64
	operations int
}

func (*crawlStateCompactionAdmissionProbe) CheckGrowth() error {
	return nil
}

func (admission *crawlStateCompactionAdmissionProbe) RunMaintenanceWithHeadroom(
	measure func() (uint64, error),
	operation func(uint64) error,
) error {
	required, err := measure()
	if err != nil {
		return err
	}
	admission.measured = required
	if admission.failure != nil {
		return admission.failure
	}
	admission.operations++

	return operation(required)
}

func TestCrawlStateCompactionHeadroomRejectionPreservesSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)
	bloatCrawlRuntimeState(t, path)
	before := crawlStateFileSize(t, path)
	original := readCrawlRuntimeStateFile(t, path)
	temporaryPath := path + crawlStateCompactionSuffix
	if err := os.WriteFile(temporaryPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale crawl-state compaction file: %v", err)
	}
	want := errors.New("compaction headroom rejected")
	admission := &crawlStateCompactionAdmissionProbe{failure: want}
	result, err := compactCrawlRuntimeState(path, before-1, admission)
	if !errors.Is(err, want) || result.installed ||
		admission.measured != uint64(max(before, 0)) ||
		admission.operations != 0 {
		t.Fatalf("rejected compaction = %+v, admission=%+v, error=%v", result, admission, err)
	}
	if after := readCrawlRuntimeStateFile(t, path); !bytes.Equal(after, original) {
		t.Fatal("headroom rejection changed authoritative crawl state")
	}
	if _, err := os.Stat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("headroom rejection left temporary state: %v", err)
	}
}

func TestCrawlStateCompactionUsesSerializedStorageMaintenance(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "fixture")
	if err := os.WriteFile(fixture, []byte("state"), 0o600); err != nil {
		t.Fatalf("write compaction fixture: %v", err)
	}
	info, err := os.Stat(fixture)
	if err != nil {
		t.Fatalf("inspect compaction fixture: %v", err)
	}
	gate := yagocrawlcontract.NewStoragePressureGate(
		t.TempDir(),
		yagocrawlcontract.StoragePressurePolicy{},
	)
	firstCopy := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, compactErr := compactCrawlRuntimeStateWithFilesystem(
			"first.db",
			1,
			crawlStateSerializationFilesystem(info, nil, func() {
				close(firstCopy)
				<-releaseFirst
			}),
			gate,
		)
		firstDone <- compactErr
	}()
	<-firstCopy
	secondMeasured := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		_, compactErr := compactCrawlRuntimeStateWithFilesystem(
			"second.db",
			1,
			crawlStateSerializationFilesystem(info, secondMeasured, nil),
			gate,
		)
		secondDone <- compactErr
	}()
	select {
	case <-secondMeasured:
		close(releaseFirst)
		t.Fatal("second compaction measured while first copy was active")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first serialized compaction: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second serialized compaction: %v", err)
	}
}

func crawlStateSerializationFilesystem(
	info os.FileInfo,
	innerMeasurement chan struct{},
	copyStarted func(),
) crawlStateCompactionFilesystem {
	inspections := 0

	return crawlStateCompactionFilesystem{
		remove: func(string) error { return os.ErrNotExist },
		inspect: func(string) (os.FileInfo, error) {
			inspections++
			if inspections == 2 && innerMeasurement != nil {
				close(innerMeasurement)
			}

			return info, nil
		},
		copy: func(string, string) error {
			if copyStarted != nil {
				copyStarted()
			}

			return nil
		},
		replace:       func(string, string) error { return nil },
		syncDirectory: func(string) error { return nil },
	}
}

func TestOpenCrawlRuntimeStateContinuesAfterRecoverableCompactionFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)
	bloatCrawlRuntimeState(t, path)
	original := readCrawlRuntimeStateFile(t, path)
	temporaryPath := path + crawlStateCompactionSuffix
	if err := os.Mkdir(temporaryPath, 0o700); err != nil {
		t.Fatalf("create blocking compaction directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(temporaryPath, "entry"), []byte("x"), 0o600); err != nil {
		t.Fatalf("populate blocking compaction directory: %v", err)
	}
	if _, err := compactCrawlRuntimeState(path, 1); err == nil {
		t.Fatal("crawl-state compaction succeeded with an unremovable temporary path")
	}
	after := readCrawlRuntimeStateFile(t, path)
	if !bytes.Equal(after, original) {
		t.Fatal("failed compaction changed the authoritative crawl state")
	}
	admission := newCrawlStateGrowthAdmission(path, 1, nil)

	state, err := openCrawlRuntimeStateStorage(path, admission)
	if err != nil {
		t.Fatalf("open original crawl state after compaction failure: %v", err)
	}
	if err := state.Close(); err != nil {
		t.Fatalf("close original crawl state: %v", err)
	}
}

func TestCompactCrawlRuntimeStateHandlesFilesystemFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)
	if err := os.WriteFile(path, []byte("state"), 0o600); err != nil {
		t.Fatalf("write crawl state fixture: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inspect crawl state fixture: %v", err)
	}
	want := errors.New("filesystem failure")
	for _, test := range crawlStateCompactionFilesystemCases(info, want) {
		t.Run(test.name, func(t *testing.T) {
			result, err := compactCrawlRuntimeStateWithFilesystem(
				path,
				test.maximum,
				test.filesystem,
			)
			if (err != nil) != test.wantError || result.installed != test.installed {
				t.Fatalf("compaction = %+v, %v", result, err)
			}
		})
	}
}

type crawlStateCompactionFilesystemTest struct {
	name       string
	maximum    int64
	filesystem crawlStateCompactionFilesystem
	wantError  bool
	installed  bool
}

func crawlStateCompactionFilesystemCases(
	info os.FileInfo,
	failure error,
) []crawlStateCompactionFilesystemTest {
	base := crawlStateCompactionFilesystem{
		remove:        func(string) error { return os.ErrNotExist },
		inspect:       func(string) (os.FileInfo, error) { return info, nil },
		copy:          func(string, string) error { return nil },
		replace:       func(string, string) error { return nil },
		syncDirectory: func(string) error { return nil },
	}
	cases := crawlStateCompactionPreInstallCases(base, failure, info.Size())

	return append(cases, crawlStateCompactionPostInstallCases(base, failure, info)...)
}

func crawlStateCompactionPreInstallCases(
	base crawlStateCompactionFilesystem,
	failure error,
	fileSize int64,
) []crawlStateCompactionFilesystemTest {
	admitted := crawlStateCompactionAdmittedCases(base, failure, fileSize)
	cases := make([]crawlStateCompactionFilesystemTest, 0, 4+len(admitted))
	cases = append(cases, []crawlStateCompactionFilesystemTest{
		{
			name: "stale cleanup", maximum: 1,
			filesystem: crawlStateCompactionFilesystem{
				remove: func(string) error { return failure },
			},
			wantError: true,
		},
		{name: "disabled", maximum: 0, filesystem: base},
		{
			name: "missing source", maximum: 1,
			filesystem: crawlStateCompactionFilesystem{
				remove: base.remove,
				inspect: func(string) (os.FileInfo, error) {
					return nil, os.ErrNotExist
				},
			},
		},
		{
			name: "source inspection", maximum: 1,
			filesystem: crawlStateCompactionFilesystem{
				remove: base.remove,
				inspect: func(string) (os.FileInfo, error) {
					return nil, failure
				},
			},
			wantError: true,
		},
	}...)

	return append(cases, admitted...)
}

func crawlStateCompactionAdmittedCases(
	base crawlStateCompactionFilesystem,
	failure error,
	fileSize int64,
) []crawlStateCompactionFilesystemTest {
	return []crawlStateCompactionFilesystemTest{
		{
			name: "source measurement", maximum: 1,
			filesystem: crawlStateCompactionFilesystem{
				remove: base.remove,
				inspect: func() func(string) (os.FileInfo, error) {
					calls := 0
					return func(string) (os.FileInfo, error) {
						calls++
						if calls == 1 {
							return base.inspect("")
						}

						return nil, failure
					}
				}(),
			},
			wantError: true,
		},
		{name: "within maximum", maximum: fileSize + 1, filesystem: base},
		{
			name: "source shrank before admitted copy", maximum: fileSize,
			filesystem: crawlStateCompactionFilesystem{
				remove: base.remove,
				inspect: func() func(string) (os.FileInfo, error) {
					calls := 0
					return func(string) (os.FileInfo, error) {
						calls++
						current, err := base.inspect("")
						if err != nil {
							return nil, err
						}
						if calls == 1 {
							return current, nil
						}

						return crawlStateSizedFileInfo{FileInfo: current}, nil
					}
				}(),
			},
		},
		{
			name: "copy", maximum: 1,
			filesystem: crawlStateCompactionFilesystem{
				remove: base.remove, inspect: base.inspect,
				copy: func(string, string) error { return failure },
			},
			wantError: true,
		},
		{
			name: "replace", maximum: 1,
			filesystem: crawlStateCompactionFilesystem{
				remove: base.remove, inspect: base.inspect, copy: base.copy,
				replace: func(string, string) error { return failure },
			},
			wantError: true,
		},
	}
}

type crawlStateSizedFileInfo struct {
	os.FileInfo
	size int64
}

func (info crawlStateSizedFileInfo) Size() int64 {
	return info.size
}

func crawlStateCompactionPostInstallCases(
	base crawlStateCompactionFilesystem,
	failure error,
	info os.FileInfo,
) []crawlStateCompactionFilesystemTest {
	return []crawlStateCompactionFilesystemTest{
		{
			name: "directory sync", maximum: 1,
			filesystem: crawlStateCompactionFilesystem{
				remove: base.remove, inspect: base.inspect, copy: base.copy,
				replace:       base.replace,
				syncDirectory: func(string) error { return failure },
			},
			wantError: true, installed: true,
		},
		{
			name: "installed inspection", maximum: 1,
			filesystem: crawlStateCompactionFilesystem{
				remove: base.remove,
				inspect: func() func(string) (os.FileInfo, error) {
					calls := 0
					return func(string) (os.FileInfo, error) {
						calls++
						if calls < 3 {
							return info, nil
						}

						return nil, failure
					}
				}(),
				copy: base.copy, replace: base.replace, syncDirectory: base.syncDirectory,
			},
			wantError: true, installed: true,
		},
		{name: "success", maximum: 1, filesystem: base, installed: true},
	}
}

func TestCrawlStateCompactionReportsInstalledDurabilityWarning(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	reportCrawlStateCompaction(
		"crawlbroker.db",
		1024,
		crawlStateCompaction{installed: true},
		errors.New("directory sync failed"),
	)
	logOutput := output.String()
	if !strings.Contains(logOutput, msgCrawlStateCompactionWarning) ||
		!strings.Contains(logOutput, `"installed":true`) {
		t.Fatalf("installed crawl-state warning = %q", logOutput)
	}
}

func TestCrawlStateCompactionReportsInstalledSuccess(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	reportCrawlStateCompaction(
		"crawlbroker.db",
		1024,
		crawlStateCompaction{installed: true, beforeBytes: 2048, afterBytes: 1024},
		nil,
	)
	logOutput := output.String()
	if !strings.Contains(logOutput, msgCrawlStateCompacted) ||
		!strings.Contains(logOutput, `"reclaimedBytes":1024`) {
		t.Fatalf("installed crawl-state report = %q", logOutput)
	}
}

func TestCopyCompactedCrawlRuntimeStateReportsOpenFailures(t *testing.T) {
	invalidPath := filepath.Join(t.TempDir(), "invalid.db")
	if err := os.WriteFile(invalidPath, []byte("invalid"), 0o600); err != nil {
		t.Fatalf("write invalid crawl state: %v", err)
	}
	if err := copyCompactedCrawlRuntimeState(invalidPath, invalidPath+".copy"); err == nil {
		t.Fatal("invalid crawl state opened for compaction")
	}
	path := filepath.Join(t.TempDir(), crawlBrokerStateFileName)
	bloatCrawlRuntimeState(t, path)
	if err := copyCompactedCrawlRuntimeState(
		path,
		filepath.Join(t.TempDir(), "missing", crawlBrokerStateFileName),
	); err == nil {
		t.Fatal("missing crawl-state compaction target directory succeeded")
	}
}

type crawlStateDirectoryFailure struct {
	syncErr  error
	closeErr error
}

func (directory crawlStateDirectoryFailure) Sync() error {
	return directory.syncErr
}

func (directory crawlStateDirectoryFailure) Close() error {
	return directory.closeErr
}

func TestCrawlRuntimeStateDirectorySyncReportsFailures(t *testing.T) {
	want := errors.New("sync failed")
	if err := finishCrawlRuntimeStateDirectorySync(crawlStateDirectoryFailure{
		syncErr:  want,
		closeErr: errors.New("close failed"),
	}); !errors.Is(err, want) {
		t.Fatalf("directory sync failure = %v", err)
	}
	if err := syncCrawlRuntimeStateDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing crawl-state directory synced")
	}
}

func bloatCrawlRuntimeState(t *testing.T, path string) {
	t.Helper()
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open crawl state: %v", err)
	}
	records, err := vault.Register(storage, "state", crawlStateValueCodec{})
	if err != nil {
		_ = storage.Close()
		t.Fatalf("register crawl state: %v", err)
	}
	value := bytes.Repeat([]byte("crawl-state"), 4096)
	if err := storage.Update(t.Context(), func(transaction *vault.Txn) error {
		if err := records.Put(transaction, vault.Key("retained"), []byte("live")); err != nil {
			return fmt.Errorf("write retained crawl state: %w", err)
		}
		for index := range 256 {
			if err := records.Put(
				transaction,
				vault.Key(fmt.Sprintf("temporary-%03d", index)),
				value,
			); err != nil {
				return fmt.Errorf("write temporary crawl state: %w", err)
			}
		}

		return nil
	}); err != nil {
		_ = storage.Close()
		t.Fatalf("grow crawl state: %v", err)
	}
	if err := storage.Update(t.Context(), func(transaction *vault.Txn) error {
		for index := range 256 {
			if _, err := records.Delete(
				transaction,
				vault.Key(fmt.Sprintf("temporary-%03d", index)),
			); err != nil {
				return fmt.Errorf("delete temporary crawl state: %w", err)
			}
		}

		return nil
	}); err != nil {
		_ = storage.Close()
		t.Fatalf("free crawl state pages: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close crawl state: %v", err)
	}
}

type crawlStateValueCodec struct{}

func (crawlStateValueCodec) Encode(value []byte) ([]byte, error) {
	return bytes.Clone(value), nil
}

func (crawlStateValueCodec) Decode(raw []byte) ([]byte, error) {
	return bytes.Clone(raw), nil
}

func readRetainedCrawlRuntimeState(t *testing.T, path string) []byte {
	t.Helper()
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open compacted crawl state: %v", err)
	}
	records, err := vault.Register(storage, "state", crawlStateValueCodec{})
	if err != nil {
		_ = storage.Close()
		t.Fatalf("register compacted crawl state: %v", err)
	}
	var value []byte
	if err := storage.View(t.Context(), func(transaction *vault.Txn) error {
		loaded, found, readErr := records.Get(transaction, vault.Key("retained"))
		if readErr != nil {
			return fmt.Errorf("read retained crawl state: %w", readErr)
		}
		if !found {
			return errors.New("retained crawl state missing")
		}
		value = loaded

		return nil
	}); err != nil {
		_ = storage.Close()
		t.Fatalf("read compacted crawl state: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close compacted crawl state: %v", err)
	}

	return value
}

func crawlStateFileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat crawl state: %v", err)
	}

	return info.Size()
}

func readCrawlRuntimeStateFile(t *testing.T, path string) []byte {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatalf("open crawl state directory: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	contents, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatalf("read crawl state: %v", err)
	}

	return contents
}
