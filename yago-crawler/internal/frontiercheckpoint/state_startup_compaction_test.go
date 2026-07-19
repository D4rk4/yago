package frontiercheckpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type allowFrontierStateMaintenance struct{}

func (allowFrontierStateMaintenance) RunMaintenanceWithHeadroom(
	measure func() (uint64, error),
	operation func(uint64) error,
) error {
	requiredBytes, err := measure()
	if err != nil {
		return err
	}

	return operation(requiredBytes)
}

func TestOpenWithStateMaximumCompactsLiveStateAndSecuresMode(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	bloatFrontierState(t, checkpoint)
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close bloated checkpoint: %v", err)
	}
	sourceInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inspect checkpoint mode: %v", err)
	}
	if err := os.Chmod(path, sourceInfo.Mode().Perm()|0o040); err != nil {
		t.Fatalf("set checkpoint mode: %v", err)
	}
	before := fileSize(t, path)
	temporaryPath := path + frontierStateCompactionSuffix
	if err := os.WriteFile(temporaryPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale compaction file: %v", err)
	}

	result, err := compactFrontierState(
		path,
		frontierStateSize(t, before-1),
		allowFrontierStateMaintenance{},
	)
	if err != nil {
		t.Fatalf("compact checkpoint: %v", err)
	}
	if !result.installed {
		t.Fatal("oversized checkpoint was not compacted")
	}
	after := fileSize(t, path)
	if after >= before {
		t.Fatalf("compacted checkpoint size = %d, want below %d", after, before)
	}
	if _, err := os.Stat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale compaction file remains: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat compacted checkpoint: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("compacted checkpoint mode = %o, want 600", info.Mode().Perm())
	}
	checkpoint, err = Open(path)
	if err != nil {
		t.Fatalf("open compacted checkpoint: %v", err)
	}
	defer func() { _ = checkpoint.Close() }()
	if err := checkpoint.readTransaction(context.Background(), func(transaction *bolt.Tx) error {
		value := transaction.Bucket(metadataBucket).Get([]byte("retained"))
		if !bytes.Equal(value, []byte("live")) {
			return fmt.Errorf("retained value = %q", value)
		}

		return nil
	}); err != nil {
		t.Fatalf("read compacted checkpoint: %v", err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("stat secured checkpoint: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("opened checkpoint mode = %o, want 600", info.Mode().Perm())
	}
}

func TestCompactFrontierStateCompactsFileAtMaximum(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	bloatFrontierState(t, checkpoint)
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint: %v", err)
	}
	before := fileSize(t, path)
	result, err := compactFrontierState(
		path,
		frontierStateSize(t, before),
		allowFrontierStateMaintenance{},
	)
	if err != nil {
		t.Fatalf("inspect frontier state at maximum: %v", err)
	}
	if !result.installed || fileSize(t, path) >= before {
		t.Fatalf("frontier state was not compacted at maximum: %+v", result)
	}
}

func TestCompactFrontierStateSkipsFileBelowMaximum(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint: %v", err)
	}
	before := fileSize(t, path)
	result, err := compactFrontierState(
		path,
		frontierStateSize(t, before+1),
		allowFrontierStateMaintenance{},
	)
	if err != nil {
		t.Fatalf("inspect frontier state below maximum: %v", err)
	}
	if result.installed || fileSize(t, path) != before {
		t.Fatalf("frontier state compacted below maximum: %+v", result)
	}
}

func TestOpenWithStateMaximumStartsAboveCompactedLiveStateMaximum(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	provenance := []byte("existing-run")
	if err := checkpoint.Begin(
		context.Background(),
		provenance,
		[]byte("existing-order"),
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("begin existing run: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint: %v", err)
	}
	checkpoint, err := OpenWithStateMaximum(path, 1, allowFrontierStateMaintenance{})
	if err != nil {
		t.Fatalf("open checkpoint above state maximum: %v", err)
	}
	defer func() { _ = checkpoint.Close() }()
	if err := checkpoint.CheckGrowth(); !errors.Is(err, ErrStateMaximum) {
		t.Fatalf("growth error = %v, want %v", err, ErrStateMaximum)
	}
	if err := checkpoint.UpdateControl(
		context.Background(),
		provenance,
		ControlUpdate{Cancelled: true},
	); err != nil {
		t.Fatalf("lifecycle write under state maximum: %v", err)
	}
}

func TestOpenWithStateMaximumContinuesAfterRecoverableCompactionFailure(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint: %v", err)
	}
	original := readFrontierStateFile(t, path)
	temporaryPath := path + frontierStateCompactionSuffix
	if err := os.Mkdir(temporaryPath, 0o700); err != nil {
		t.Fatalf("create blocking compaction directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(temporaryPath, "entry"), []byte("x"), 0o600); err != nil {
		t.Fatalf("populate blocking compaction directory: %v", err)
	}
	if _, err := compactFrontierState(path, 1, allowFrontierStateMaintenance{}); err == nil {
		t.Fatal("frontier compaction succeeded with an unremovable temporary path")
	}
	after := readFrontierStateFile(t, path)
	if !bytes.Equal(after, original) {
		t.Fatal("failed compaction changed the authoritative checkpoint")
	}

	checkpoint, err := OpenWithStateMaximum(path, 1, allowFrontierStateMaintenance{})
	if err != nil {
		t.Fatalf("open original after compaction failure: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close original after compaction failure: %v", err)
	}
}

func TestCompactFrontierStateHandlesFilesystemFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.db")
	if err := os.WriteFile(path, []byte("state"), 0o600); err != nil {
		t.Fatalf("write frontier state fixture: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inspect frontier state fixture: %v", err)
	}
	want := errors.New("filesystem failure")
	for _, test := range frontierStateCompactionFilesystemCases(info, want) {
		t.Run(test.name, func(t *testing.T) {
			result, err := compactFrontierStateWithFilesystem(
				path,
				test.maximum,
				allowFrontierStateMaintenance{},
				test.filesystem,
			)
			if (err != nil) != test.wantError || result.installed != test.installed {
				t.Fatalf("compaction = %+v, %v", result, err)
			}
		})
	}
}

type frontierStateCompactionFilesystemTest struct {
	name       string
	maximum    uint64
	filesystem frontierStateCompactionFilesystem
	wantError  bool
	installed  bool
}

func frontierStateCompactionFilesystemCases(
	info os.FileInfo,
	failure error,
) []frontierStateCompactionFilesystemTest {
	base := frontierStateCompactionFilesystem{
		remove:        func(string) error { return os.ErrNotExist },
		inspect:       func(string) (os.FileInfo, error) { return info, nil },
		copy:          func(string, string) error { return nil },
		replace:       func(string, string) error { return nil },
		syncDirectory: func(string) error { return nil },
	}
	cases := frontierStateCompactionPreInstallCases(base, failure)

	return append(cases, frontierStateCompactionPostInstallCases(base, failure, info)...)
}

func frontierStateCompactionPreInstallCases(
	base frontierStateCompactionFilesystem,
	failure error,
) []frontierStateCompactionFilesystemTest {
	return []frontierStateCompactionFilesystemTest{
		{
			name: "stale cleanup", maximum: 1,
			filesystem: frontierStateCompactionFilesystem{
				remove: func(string) error { return failure },
			},
			wantError: true,
		},
		{name: "disabled", maximum: 0, filesystem: base},
		{
			name: "missing source", maximum: 1,
			filesystem: frontierStateCompactionFilesystem{
				remove: base.remove,
				inspect: func(string) (os.FileInfo, error) {
					return nil, os.ErrNotExist
				},
			},
		},
		{
			name: "source inspection", maximum: 1,
			filesystem: frontierStateCompactionFilesystem{
				remove: base.remove,
				inspect: func(string) (os.FileInfo, error) {
					return nil, failure
				},
			},
			wantError: true,
		},
		{name: "within maximum", maximum: math.MaxInt64, filesystem: base},
		{name: "maximum beyond signed range", maximum: math.MaxUint64, filesystem: base},
		{
			name: "copy", maximum: 1,
			filesystem: frontierStateCompactionFilesystem{
				remove: base.remove, inspect: base.inspect,
				copy: func(string, string) error { return failure },
			},
			wantError: true,
		},
		{
			name: "replace", maximum: 1,
			filesystem: frontierStateCompactionFilesystem{
				remove: base.remove, inspect: base.inspect, copy: base.copy,
				replace: func(string, string) error { return failure },
			},
			wantError: true,
		},
	}
}

func frontierStateCompactionPostInstallCases(
	base frontierStateCompactionFilesystem,
	failure error,
	info os.FileInfo,
) []frontierStateCompactionFilesystemTest {
	installedInspections := 0
	installedInspection := func(string) (os.FileInfo, error) {
		installedInspections++
		if installedInspections <= 2 {
			return info, nil
		}

		return nil, failure
	}

	return []frontierStateCompactionFilesystemTest{
		{
			name: "directory sync", maximum: 1,
			filesystem: frontierStateCompactionFilesystem{
				remove: base.remove, inspect: base.inspect, copy: base.copy,
				replace:       base.replace,
				syncDirectory: func(string) error { return failure },
			},
			wantError: true, installed: true,
		},
		{
			name: "installed inspection", maximum: 1,
			filesystem: frontierStateCompactionFilesystem{
				remove: base.remove, inspect: installedInspection,
				copy: base.copy, replace: base.replace, syncDirectory: base.syncDirectory,
			},
			wantError: true, installed: true,
		},
		{name: "success", maximum: 1, filesystem: base, installed: true},
	}
}

func TestFrontierStateCompactionReportsInstalledDurabilityWarning(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	reportFrontierStateCompaction(
		"frontier.db",
		1024,
		frontierStateCompaction{installed: true},
		errors.New("directory sync failed"),
	)
	logOutput := output.String()
	if !strings.Contains(logOutput, msgStateCompactWarned) ||
		!strings.Contains(logOutput, `"installed":true`) {
		t.Fatalf("installed compaction warning = %q", logOutput)
	}
}

func TestCopyCompactedFrontierStateReportsOpenAndCopyFailures(t *testing.T) {
	invalidPath := filepath.Join(t.TempDir(), "invalid.db")
	if err := os.WriteFile(invalidPath, []byte("invalid"), 0o600); err != nil {
		t.Fatalf("write invalid frontier state: %v", err)
	}
	if err := copyCompactedFrontierState(invalidPath, invalidPath+".copy"); err == nil {
		t.Fatal("invalid frontier state opened for compaction")
	}
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint: %v", err)
	}
	if err := copyCompactedFrontierState(
		path,
		filepath.Join(t.TempDir(), "missing", "frontier.db"),
	); err == nil {
		t.Fatal("missing compaction target directory succeeded")
	}
	source, err := bolt.Open(path, 0o600, &bolt.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("open source checkpoint: %v", err)
	}
	temporaryPath := filepath.Join(t.TempDir(), "copy.db")
	destination, err := bolt.Open(temporaryPath, 0o600, nil)
	if err != nil {
		_ = source.Close()
		t.Fatalf("open destination checkpoint: %v", err)
	}
	want := errors.New("copy failed")
	if err := finishFrontierStateCopy(
		destination,
		source,
		temporaryPath,
		func(*bolt.DB, *bolt.DB, int64) error { return want },
		os.Chmod,
	); !errors.Is(err, want) {
		t.Fatalf("copy failure = %v", err)
	}
}

type frontierStateDirectoryFailure struct {
	syncErr  error
	closeErr error
}

func (directory frontierStateDirectoryFailure) Sync() error {
	return directory.syncErr
}

func (directory frontierStateDirectoryFailure) Close() error {
	return directory.closeErr
}

func TestFrontierStateDirectorySyncReportsFailures(t *testing.T) {
	want := errors.New("sync failed")
	if err := finishFrontierStateDirectorySync(frontierStateDirectoryFailure{
		syncErr:  want,
		closeErr: errors.New("close failed"),
	}); !errors.Is(err, want) {
		t.Fatalf("directory sync failure = %v", err)
	}
	if err := syncFrontierStateDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing directory synced")
	}
}

func bloatFrontierState(t *testing.T, checkpoint *FrontierCheckpoint) {
	t.Helper()
	value := bytes.Repeat([]byte("frontier-state"), 4096)
	if err := checkpoint.database.Update(func(transaction *bolt.Tx) error {
		metadata := transaction.Bucket(metadataBucket)
		if err := metadata.Put([]byte("retained"), []byte("live")); err != nil {
			return fmt.Errorf("write retained frontier state: %w", err)
		}
		for index := range 256 {
			if err := metadata.Put(
				[]byte(fmt.Sprintf("temporary-%03d", index)),
				value,
			); err != nil {
				return fmt.Errorf("write temporary frontier state: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("grow checkpoint: %v", err)
	}
	if err := checkpoint.database.Update(func(transaction *bolt.Tx) error {
		metadata := transaction.Bucket(metadataBucket)
		for index := range 256 {
			if err := metadata.Delete([]byte(fmt.Sprintf("temporary-%03d", index))); err != nil {
				return fmt.Errorf("delete temporary frontier state: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("free checkpoint pages: %v", err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	return info.Size()
}

func frontierStateSize(t *testing.T, size int64) uint64 {
	t.Helper()
	stateBytes, err := strconv.ParseUint(strconv.FormatInt(size, 10), 10, 64)
	if err != nil {
		t.Fatalf("convert frontier state size: %v", err)
	}

	return stateBytes
}

func readFrontierStateFile(t *testing.T, path string) []byte {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatalf("open frontier state directory: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	contents, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatalf("read frontier state: %v", err)
	}

	return contents
}
