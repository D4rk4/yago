package adminui

import (
	"context"
	"fmt"
	"net/http"
)

const backupPath = "/admin/backup"

// BackupStatus is what the Backup & restore page shows: where the durable state
// lives and how much of the storage quota it uses.
type BackupStatus struct {
	DataDir    string
	UsedBytes  int64
	QuotaBytes int64
}

// BackupSource reports the node's storage status for the Backup & restore page;
// the node wires it over the vault.
type BackupSource interface {
	BackupStatus(ctx context.Context) (BackupStatus, error)
}

type backupPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	Available  bool
	Error      string
	DataDir    string
	Used       string
	Quota      string
	// DockerBackup through SystemdRestore are the exact operator command lines
	// for the offline scripts, pre-filled with this node's data directory.
	DockerBackup   string
	DockerRestore  string
	SystemdBackup  string
	SystemdRestore string
}

func (c *Console) handleBackup(w http.ResponseWriter, r *http.Request) {
	if c.backup == nil {
		c.renderUnavailable(w, r, backupPath, "Backup & restore",
			"Storage status is not available on this deployment.")

		return
	}
	data := backupPageData{
		AppName: appName, ActivePath: backupPath, Nav: navItems,
		CSRF:    csrfToken(r),
		Section: sectionView{Heading: "Backup & restore", Available: true},
	}
	status, err := c.backup.BackupStatus(r.Context())
	if err != nil {
		data.Error = "Reading the storage status failed: " + err.Error()
	} else {
		data.Available = true
		data.DataDir = status.DataDir
		data.Used = formatByteSize(status.UsedBytes)
		data.Quota = formatStorageQuota(status.QuotaBytes)
		data.DockerBackup = "deploy/backup.sh docker docker-compose.yml yago-node yago_yago-data ./backups yago-crawler yago_yago-crawler-data"
		data.DockerRestore = "deploy/restore.sh docker docker-compose.yml yago-node yago_yago-data ./backups/<archive>.tar.gz yago-crawler yago_yago-crawler-data"
		data.SystemdBackup = fmt.Sprintf(
			"deploy/backup.sh systemd yago-node.service %s ./backups yago-crawler.service",
			status.DataDir,
		)
		data.SystemdRestore = fmt.Sprintf(
			"deploy/restore.sh systemd yago-node.service %s ./backups/<archive>.tar.gz yago-crawler.service",
			status.DataDir,
		)
	}
	c.render(r.Context(), w, c.tpl.backup, "layout", data)
}

func formatByteSize(count int64) string {
	if count <= 0 {
		return "0 B"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(count)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}

	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func formatStorageQuota(count int64) string {
	if count <= 0 {
		return "unlimited"
	}

	return formatByteSize(count)
}
