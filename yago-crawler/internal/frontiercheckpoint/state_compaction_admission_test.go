package frontiercheckpoint

import (
	"errors"
	"os"
	"testing"
)

func TestFrontierStateCompactionMeasuresSourceInsideMaintenanceAdmission(t *testing.T) {
	initial := frontierStateInfoWithSize(t, 64)
	measured := frontierStateInfoWithSize(t, 96)
	installed := frontierStateInfoWithSize(t, 48)
	inspections := 0
	maintenance := &frontierStateMaintenanceRecorder{}
	filesystem := frontierStateCompactionFilesystem{
		remove: func(string) error { return os.ErrNotExist },
		inspect: func(string) (os.FileInfo, error) {
			inspections++
			switch inspections {
			case 1:
				return initial, nil
			case 2:
				return measured, nil
			default:
				return installed, nil
			}
		},
		copy: func(string, string) error {
			if !maintenance.active {
				t.Fatal("compacted copy ran outside maintenance admission")
			}
			maintenance.copyObserved = true

			return nil
		},
		replace:       func(string, string) error { return nil },
		syncDirectory: func(string) error { return nil },
	}
	result, err := compactFrontierStateWithFilesystem(
		"frontier.db",
		1,
		maintenance,
		filesystem,
	)
	if err != nil {
		t.Fatalf("compact admitted frontier state: %v", err)
	}
	if maintenance.requiredBytes != 96 || !maintenance.copyObserved {
		t.Fatalf("maintenance observation = %+v", maintenance)
	}
	if !result.installed || result.beforeBytes != 64 || result.afterBytes != 48 {
		t.Fatalf("frontier state compaction = %+v", result)
	}
}

func TestFrontierStateCompactionHeadroomRefusalPreservesSource(t *testing.T) {
	want := errors.New("headroom refused")
	info := frontierStateInfoWithSize(t, 64)
	copyCalled := false
	removeCalls := 0
	maintenance := &frontierStateMaintenanceRecorder{reject: want}
	result, err := compactFrontierStateWithFilesystem(
		"frontier.db",
		1,
		maintenance,
		frontierStateCompactionFilesystem{
			remove: func(string) error {
				removeCalls++

				return os.ErrNotExist
			},
			inspect: func(string) (os.FileInfo, error) { return info, nil },
			copy: func(string, string) error {
				copyCalled = true

				return nil
			},
		},
	)
	if !errors.Is(err, want) || result.installed || copyCalled {
		t.Fatalf("refused compaction = %+v, %v, copy=%t", result, err, copyCalled)
	}
	if maintenance.requiredBytes != 64 || removeCalls != 2 {
		t.Fatalf("refused maintenance = %+v, removes=%d", maintenance, removeCalls)
	}
}

func TestFrontierStateCompactionRequiresMeasuredMaintenanceAdmission(t *testing.T) {
	info := frontierStateInfoWithSize(t, 64)
	base := frontierStateCompactionFilesystem{
		remove:  func(string) error { return os.ErrNotExist },
		inspect: func(string) (os.FileInfo, error) { return info, nil },
		copy:    func(string, string) error { return nil },
	}
	if _, err := compactFrontierStateWithFilesystem(
		"frontier.db",
		1,
		nil,
		base,
	); err == nil {
		t.Fatal("frontier state compaction without maintenance admission succeeded")
	}

	want := errors.New("measurement failed")
	inspections := 0
	base.inspect = func(string) (os.FileInfo, error) {
		inspections++
		if inspections == 1 {
			return info, nil
		}

		return nil, want
	}
	if _, err := compactFrontierStateWithFilesystem(
		"frontier.db",
		1,
		&frontierStateMaintenanceRecorder{},
		base,
	); !errors.Is(err, want) {
		t.Fatalf("frontier state headroom measurement failure = %v", err)
	}

	negative := frontierStateSizedInfo{FileInfo: info, size: -1}
	inspections = 0
	base.inspect = func(string) (os.FileInfo, error) {
		inspections++
		if inspections == 1 {
			return info, nil
		}

		return negative, nil
	}
	if _, err := compactFrontierStateWithFilesystem(
		"frontier.db",
		1,
		&frontierStateMaintenanceRecorder{},
		base,
	); err == nil {
		t.Fatal("negative frontier state source size was admitted")
	}
}

type frontierStateMaintenanceRecorder struct {
	active        bool
	copyObserved  bool
	requiredBytes uint64
	reject        error
}

func (recorder *frontierStateMaintenanceRecorder) RunMaintenanceWithHeadroom(
	measure func() (uint64, error),
	operation func(uint64) error,
) error {
	requiredBytes, err := measure()
	if err != nil {
		return err
	}
	recorder.requiredBytes = requiredBytes
	if recorder.reject != nil {
		return recorder.reject
	}
	recorder.active = true
	err = operation(requiredBytes)
	recorder.active = false

	return err
}

type frontierStateSizedInfo struct {
	os.FileInfo
	size int64
}

func (info frontierStateSizedInfo) Size() int64 {
	return info.size
}

func frontierStateInfoWithSize(t *testing.T, size int64) os.FileInfo {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "state-*")
	if err != nil {
		t.Fatalf("open state size fixture: %v", err)
	}
	path := file.Name()
	if err := file.Chmod(frontierStateFileMode); err != nil {
		_ = file.Close()
		t.Fatalf("secure state size fixture: %v", err)
	}
	if err := file.Truncate(size); err != nil {
		_ = file.Close()
		t.Fatalf("size state fixture: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close state size fixture: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state size fixture: %v", err)
	}

	return info
}
