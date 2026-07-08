package adminui

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type fakeBackup struct {
	status BackupStatus
	err    error
}

func (f fakeBackup) BackupStatus(context.Context) (BackupStatus, error) {
	return f.status, f.err
}

func TestBackupPageRendersStatusAndScriptCommands(t *testing.T) {
	t.Parallel()

	console := New(Options{Backup: fakeBackup{status: BackupStatus{
		DataDir:    "/opt/yago/data",
		UsedBytes:  1536,
		QuotaBytes: 2 << 30,
	}}})
	got := do(t, console, "/admin/backup")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d", got.status)
	}
	for _, want := range []string{
		"Backup &amp; restore",
		"/opt/yago/data",
		"1.5 KiB",
		"2.0 GiB",
		"deploy/backup.sh docker docker-compose.yml yago-node yago-data ./backups",
		"deploy/backup.sh systemd yago-node /opt/yago/data ./backups",
		"deploy/restore.sh systemd yago-node /opt/yago/data",
		"doc/backup-restore.md",
	} {
		if !strings.Contains(got.body, want) {
			t.Errorf("backup page missing %q", want)
		}
	}
}

func TestBackupPageSurfacesStatusFailure(t *testing.T) {
	t.Parallel()

	console := New(Options{Backup: fakeBackup{err: errors.New("vault closed")}})
	got := do(t, console, "/admin/backup")
	if !strings.Contains(got.body, "Reading the storage status failed") {
		t.Fatal("status failure must be surfaced")
	}
}

func TestBackupPageUnavailableWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{}), "/admin/backup")
	if got.status != http.StatusOK || !strings.Contains(got.body, "not available") {
		t.Fatalf("unavailable page = %d", got.status)
	}
}

func TestFormatByteSize(t *testing.T) {
	t.Parallel()

	for count, want := range map[int64]string{
		0:              "unlimited",
		-1:             "unlimited",
		512:            "512.0 B",
		1536:           "1.5 KiB",
		5 << 20:        "5.0 MiB",
		3 << 40:        "3.0 TiB",
		int64(2) << 50: "2048.0 TiB",
	} {
		if got := formatByteSize(count); got != want {
			t.Errorf("formatByteSize(%d) = %q, want %q", count, got, want)
		}
	}
}
