package searchindex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type rebuildDocumentDirectory struct {
	total int
	err   error
}

func (d rebuildDocumentDirectory) Document(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	return documentstore.Document{}, false, nil
}

func (d rebuildDocumentDirectory) Count(context.Context) (int, error) {
	return d.total, d.err
}

type rebuildAdmissionProbe struct {
	growthCalls   int
	headroomCalls int
	requiredBytes uint64
	err           error
	observation   BleveRebuildStorageObservation
}

type rebuildFootprintFileInfo struct {
	size int64
	mode fs.FileMode
}

func (info rebuildFootprintFileInfo) Name() string       { return "footprint" }
func (info rebuildFootprintFileInfo) Size() int64        { return info.size }
func (info rebuildFootprintFileInfo) Mode() fs.FileMode  { return info.mode }
func (info rebuildFootprintFileInfo) ModTime() time.Time { return time.Time{} }
func (info rebuildFootprintFileInfo) IsDir() bool        { return info.mode.IsDir() }
func (info rebuildFootprintFileInfo) Sys() any           { return nil }

type rebuildFootprintEntry struct {
	info fs.FileInfo
	err  error
}

func (entry rebuildFootprintEntry) Name() string { return "footprint" }
func (entry rebuildFootprintEntry) IsDir() bool {
	return entry.info != nil && entry.info.IsDir()
}

func (entry rebuildFootprintEntry) Type() fs.FileMode {
	if entry.info == nil {
		return 0
	}

	return entry.info.Mode().Type()
}

func (entry rebuildFootprintEntry) Info() (fs.FileInfo, error) {
	return entry.info, entry.err
}

func (a *rebuildAdmissionProbe) CheckGrowth() error {
	a.growthCalls++

	return a.err
}

func (a *rebuildAdmissionProbe) CheckGrowthWithHeadroom(required uint64) error {
	a.headroomCalls++
	a.requiredBytes = required

	return a.err
}

func (a *rebuildAdmissionProbe) RebuildStorageObservation() BleveRebuildStorageObservation {
	return a.observation
}

func TestBleveRebuildPreflightPrecedesDestructiveRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(root, "retained")
	if err := os.WriteFile(retained, bytes.Repeat([]byte{'x'}, 73), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireBleveRebuild(root); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("headroom rejected")
	admission := &rebuildAdmissionProbe{err: sentinel}
	index, err := NewBleveDiskIndex(
		t.Context(),
		root,
		rebuildDocumentDirectory{total: 4},
		&fakeStoredDocuments{},
		admission,
	)
	if index != nil || !errors.Is(err, sentinel) {
		t.Fatalf("preflight index = %v, error = %v", index, err)
	}
	if admission.headroomCalls != 1 || admission.growthCalls != 0 ||
		admission.requiredBytes != 73 {
		t.Fatalf("admission = %+v", admission)
	}
	if _, err := os.Stat(retained); err != nil {
		t.Fatalf("preflight removed the existing index: %v", err)
	}
	if pending, err := bleveRebuildPending(root); err != nil || !pending {
		t.Fatalf("rebuild marker pending = %v, error = %v", pending, err)
	}
}

func TestBleveRebuildLogsBoundedStructuredProgress(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "first"),
		bytes.Repeat([]byte{'a'}, 31),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "nested", "second"),
		bytes.Repeat([]byte{'b'}, 11),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	admission := &rebuildAdmissionProbe{observation: BleveRebuildStorageObservation{
		AvailableBytes: 1_000, ReservedBytes: 100, MeasurementAvailable: true,
	}}
	coordinator := newBleveRebuildCoordinator(
		root,
		rebuildDocumentDirectory{total: 100},
		admission,
	)
	startedAt := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(1250 * time.Millisecond)
	times := []time.Time{startedAt, completedAt}
	coordinator.now = func() time.Time {
		current := times[0]
		times = times[1:]

		return current
	}
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	if err := coordinator.prepare(t.Context()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := coordinator.prepare(t.Context()); err != nil {
		t.Fatalf("repeat prepare: %v", err)
	}
	for range 100 {
		if err := coordinator.CheckGrowth(); err != nil {
			t.Fatalf("batch growth: %v", err)
		}
		coordinator.BleveRebuildBatchIndexed(1)
	}
	coordinator.complete(t.Context())

	logs := output.String()
	if strings.Count(logs, `"msg":"`+bleveRebuildStartedMessage+`"`) != 1 ||
		strings.Count(logs, `"msg":"`+bleveRebuildProgressMessage+`"`) != 9 ||
		strings.Count(logs, `"msg":"`+bleveRebuildCompletedMessage+`"`) != 1 {
		t.Fatalf("rebuild logs are not bounded: %s", logs)
	}
	for _, want := range []string{
		`"documentsTotal":100`,
		`"estimatedRebuildBytes":42`,
		`"storageAvailableBytes":1000`,
		`"storageReservedBytes":100`,
		`"storageHeadroomBytes":900`,
		`"percent":10`,
		`"percent":90`,
		`"documentsIndexed":100`,
		`"batches":100`,
		`"durationMilliseconds":1250`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("rebuild logs missing %s: %s", want, logs)
		}
	}
	if admission.headroomCalls != 1 || admission.requiredBytes != 42 ||
		admission.growthCalls != 100 {
		t.Fatalf("rebuild admission = %+v", admission)
	}
}

func TestBleveRebuildPreflightRejectsUnavailableDocumentTotal(t *testing.T) {
	sentinel := errors.New("count unavailable")
	coordinator := newBleveRebuildCoordinator(
		filepath.Join(t.TempDir(), "search.bleve"),
		rebuildDocumentDirectory{err: sentinel},
		&rebuildAdmissionProbe{},
	)
	if err := coordinator.prepare(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("preflight count error = %v", err)
	}
	if coordinator.prepared {
		t.Fatal("failed preflight became prepared")
	}
}

func TestBleveRebuildFootprintAvailability(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if bytes, available, err := bleveRebuildFootprint(missing); err != nil ||
		available || bytes != 0 {
		t.Fatalf("missing footprint = %d, %v, %v", bytes, available, err)
	}
	file := filepath.Join(t.TempDir(), "legacy")
	if err := os.WriteFile(file, bytes.Repeat([]byte{'x'}, 17), 0o600); err != nil {
		t.Fatal(err)
	}
	if bytes, available, err := bleveRebuildFootprint(file); err != nil ||
		!available || bytes != 17 {
		t.Fatalf("file footprint = %d, %v, %v", bytes, available, err)
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bleveRebuildFootprint(filepath.Join(blocked, "child")); err == nil {
		t.Fatal("blocked footprint succeeded")
	}
}

func TestBleveRebuildCoordinatorHandlesBoundaryStates(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	cancelledContext, cancel := context.WithCancel(t.Context())
	cancel()
	coordinator := newBleveRebuildCoordinator(root, rebuildDocumentDirectory{}, nil)
	if err := coordinator.prepare(cancelledContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preflight error = %v", err)
	}

	loop := filepath.Join(t.TempDir(), "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatalf("create footprint symlink loop: %v", err)
	}
	coordinator = newBleveRebuildCoordinator(loop, rebuildDocumentDirectory{}, nil)
	if err := coordinator.prepare(t.Context()); err == nil {
		t.Fatal("uninspectable footprint passed preflight")
	}

	want := errors.New("storage unavailable")
	admission := &rebuildAdmissionProbe{err: want}
	coordinator = newBleveRebuildCoordinator(root, rebuildDocumentDirectory{}, admission)
	if err := coordinator.prepare(t.Context()); !errors.Is(err, want) {
		t.Fatalf("fallback admission error = %v", err)
	}
	coordinator.BleveRebuildBatchIndexed(0)
	coordinator.BleveRebuildBatchIndexed(1)
	if coordinator.batches != 1 || coordinator.documentsIndexed != 1 {
		t.Fatalf("unknown-total progress = %+v", coordinator)
	}
	if bleveRebuildMilestone(0, 1) != 0 || bleveRebuildMilestone(1, 0) != 0 {
		t.Fatal("invalid progress produced a milestone")
	}

	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	coordinator.startedAt = time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	coordinator.now = func() time.Time { return coordinator.startedAt.Add(-time.Second) }
	coordinator.complete(t.Context())
	if !strings.Contains(output.String(), `"durationMilliseconds":0`) {
		t.Fatalf("negative rebuild duration was not clamped: %s", output.String())
	}
}

func TestBleveRebuildFootprintTraversalFailures(t *testing.T) {
	directoryInfo, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatalf("inspect footprint directory: %v", err)
	}
	want := errors.New("traversal failed")
	tests := []struct {
		name  string
		entry fs.DirEntry
		err   error
	}{
		{name: "walk", err: want},
		{name: "entry", entry: rebuildFootprintEntry{err: want}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, footprintErr := bleveRebuildFootprintWithFilesystem(
				"search.bleve",
				bleveRebuildFootprintFilesystem{
					inspect: func(string) (fs.FileInfo, error) { return directoryInfo, nil },
					walk: func(root string, visit fs.WalkDirFunc) error {
						if visitErr := visit(root, test.entry, test.err); visitErr != nil {
							return fmt.Errorf("fixture traversal: %w", visitErr)
						}

						return nil
					},
				},
			)
			if !errors.Is(footprintErr, want) {
				t.Fatalf("footprint traversal error = %v", footprintErr)
			}
		})
	}
}

func TestBleveRebuildFootprintSaturatesAndIgnoresNonFiles(t *testing.T) {
	directory := rebuildFootprintFileInfo{mode: fs.ModeDir}
	files := []fs.FileInfo{
		rebuildFootprintFileInfo{size: math.MaxInt64},
		rebuildFootprintFileInfo{size: math.MaxInt64},
		rebuildFootprintFileInfo{size: 2},
	}
	bytes, available, err := bleveRebuildFootprintWithFilesystem(
		"search.bleve",
		bleveRebuildFootprintFilesystem{
			inspect: func(string) (fs.FileInfo, error) { return directory, nil },
			walk: func(root string, visit fs.WalkDirFunc) error {
				for _, info := range files {
					visitErr := visit(root, rebuildFootprintEntry{info: info}, nil)
					if errors.Is(visitErr, fs.SkipAll) {
						return nil
					}
					if visitErr != nil {
						return fmt.Errorf("fixture traversal: %w", visitErr)
					}
				}

				return nil
			},
		},
	)
	if err != nil || !available || bytes != math.MaxUint64 {
		t.Fatalf("saturated footprint = %d, %v, %v", bytes, available, err)
	}
	if regularFileBytes(directory) != 0 ||
		regularFileBytes(rebuildFootprintFileInfo{mode: 0, size: 0}) != 0 {
		t.Fatal("non-file footprint contributed bytes")
	}
}
