package searchindex

import (
	"path/filepath"
	"testing"
)

func TestDiskRebuildSchedulePersistsIdempotently(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")

	pending, err := DiskRebuildPending(root)
	if err != nil || pending {
		t.Fatalf("initial pending = %v, err = %v", pending, err)
	}
	if err := ScheduleDiskRebuild(root); err != nil {
		t.Fatalf("schedule rebuild: %v", err)
	}
	if err := ScheduleDiskRebuild(root); err != nil {
		t.Fatalf("reschedule rebuild: %v", err)
	}
	pending, err = DiskRebuildPending(root)
	if err != nil || !pending {
		t.Fatalf("scheduled pending = %v, err = %v", pending, err)
	}
}

func TestDiskRebuildScheduleRejectsEmptyPath(t *testing.T) {
	if _, err := DiskRebuildPending("  "); err == nil {
		t.Fatal("empty pending path accepted")
	}
	if err := ScheduleDiskRebuild(""); err == nil {
		t.Fatal("empty schedule path accepted")
	}
}
