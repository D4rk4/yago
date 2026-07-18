//go:build e2e

package e2e

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	backupNodeHash            = yagomodel.Hash("DEFGHIJKLMNO")
	backupVolumeImage         = "alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b"
	crawlerCheckpointPath     = "crawler/frontier-v1.db"
	crawlerCheckpointSentinel = "yago crawler frontier checkpoint sentinel v1\n"
)

func TestSystemdRestoreRejectsFilesystemRoot(t *testing.T) {
	repoRoot := repositoryRoot(t)
	command := exec.Command(
		"sh",
		filepath.Join(repoRoot, "deploy", "restore.sh"),
		"systemd",
		"yago-node.service",
		"/",
		"/dev/null",
	)
	command.Dir = repoRoot
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("restore accepted the filesystem root as its data directory")
	}
	if !strings.Contains(
		string(output),
		"refusing to restore over unsafe systemd data directory: /",
	) {
		t.Fatalf("restore rejection = %q", output)
	}
}

func TestRestoreRejectsUnreadableArchiveBeforeComposeAccess(t *testing.T) {
	repoRoot := repositoryRoot(t)
	archive := filepath.Join(t.TempDir(), "truncated.tar.gz")
	if err := os.WriteFile(archive, []byte("not a gzip archive"), 0o600); err != nil {
		t.Fatalf("write truncated archive: %v", err)
	}
	output := failedRestore(
		t,
		repoRoot,
		"docker",
		"/missing/compose.yaml",
		"node",
		"missing-node-volume",
		archive,
	)
	if !strings.Contains(output, "backup archive is unreadable or truncated") {
		t.Fatalf("restore rejection = %q", output)
	}
}

func TestRestoreRejectsUnsafeArchiveBeforeSystemdAccess(t *testing.T) {
	repoRoot := repositoryRoot(t)
	dataDirectory := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dataDirectory, 0o700); err != nil {
		t.Fatalf("create data directory: %v", err)
	}
	archive := writeRestoreArchive(t, restoreArchiveEntry{
		name: "../outside",
		body: "unsafe",
	})
	output := failedRestore(
		t,
		repoRoot,
		"systemd",
		"missing-node.service",
		dataDirectory,
		archive,
	)
	if !strings.Contains(output, "backup archive contains an unsafe or empty path set") {
		t.Fatalf("restore rejection = %q", output)
	}
}

func TestRestoreSurfacesSystemdRestartFailure(t *testing.T) {
	repoRoot := repositoryRoot(t)
	root := t.TempDir()
	dataDirectory := filepath.Join(root, "data")
	if err := os.Mkdir(dataDirectory, 0o700); err != nil {
		t.Fatalf("create data directory: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dataDirectory, "previous"),
		[]byte("old"),
		0o600,
	); err != nil {
		t.Fatalf("write previous data: %v", err)
	}
	archive := writeRestoreArchive(t, restoreArchiveEntry{name: "restored", body: "new"})
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatalf("create command directory: %v", err)
	}
	logPath := filepath.Join(root, "systemctl.log")
	systemctl := `#!/bin/sh
printf '%s\n' "$*" >> "$YAGO_SYSTEMCTL_LOG"
case "$1" in
is-active|stop) exit 0 ;;
start) [ "$2" != "yago-node.service" ] ;;
*) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(systemctl), 0o700); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}
	command := exec.Command(
		"sh",
		filepath.Join(repoRoot, "deploy", "restore.sh"),
		"systemd",
		"yago-node.service",
		dataDirectory,
		archive,
		"yago-crawler.service",
	)
	command.Dir = repoRoot
	command.Env = append(
		os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"YAGO_SYSTEMCTL_LOG="+logPath,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("restore hid restart failure: %s", output)
	}
	if !strings.Contains(string(output), "failed to restart systemd unit: yago-node.service") ||
		strings.Contains(string(output), "restore complete") {
		t.Fatalf("restore failure output = %q", output)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read systemctl log: %v", err)
	}
	if strings.Contains(string(logData), "start yago-crawler.service") {
		t.Fatalf("crawler started after node restart failure: %s", logData)
	}
	newData, err := os.ReadFile(filepath.Join(dataDirectory, "restored"))
	if err != nil || string(newData) != "new" {
		t.Fatalf("restored data = %q, %v", newData, err)
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, "previous")); !os.IsNotExist(err) {
		t.Fatalf("previous data remained after committed restore: %v", err)
	}
}

func TestRestoreRejectsMismatchedDockerLayoutBeforeComposeAccess(t *testing.T) {
	repoRoot := repositoryRoot(t)
	archive := writeRestoreArchive(t, restoreArchiveEntry{
		name: "node/data",
		body: "node-only",
	})
	output := failedRestore(
		t,
		repoRoot,
		"docker",
		"/missing/compose.yaml",
		"node",
		"missing-node-volume",
		archive,
		"crawler",
		"missing-crawler-volume",
	)
	if !strings.Contains(output, "dual-volume backup must contain only node/ and crawler/") {
		t.Fatalf("restore rejection = %q", output)
	}
}

func TestRestoreRejectsArchiveLinksBeforeComposeAccess(t *testing.T) {
	repoRoot := repositoryRoot(t)
	archive := writeRestoreArchive(t, restoreArchiveEntry{
		name:     "link",
		linkname: "/tmp/outside",
		kind:     tar.TypeSymlink,
	})
	output := failedRestore(
		t,
		repoRoot,
		"docker",
		"/missing/compose.yaml",
		"node",
		"missing-node-volume",
		archive,
	)
	if !strings.Contains(output, "backup archive contains unsupported links or special files") {
		t.Fatalf("restore rejection = %q", output)
	}
}

func TestBackupRestoreRoundTripPreservesIndex(t *testing.T) {
	ctx := context.Background()
	probe := newHTTPProbe(t)
	repoRoot := repositoryRoot(t)
	project := fmt.Sprintf("yagobackup%d", os.Getpid())
	workdir := filepath.Join(t.TempDir(), project)
	if err := os.MkdirAll(workdir, 0o700); err != nil {
		t.Fatalf("workdir: %v", err)
	}
	nodeVolume := project + "-node-data"
	crawlerVolume := project + "-crawler-data"
	composePath := writeBackupCompose(t, workdir, nodeVolume, crawlerVolume)
	defer composeDown(t, composePath)

	composeRun(t, composePath, "up", "-d")
	writeCrawlerCheckpointSentinel(t, composePath)
	nodeURL := backupNodeURL(t, composePath)
	if !waitFor(30*time.Second, func() bool {
		return probe.OK(ctx, nodeURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatal("node never became reachable before the backup")
	}

	words := make([]string, 0, 24)
	for i := range 24 {
		words = append(words, fmt.Sprintf("backupword%02d", i))
	}
	seedNodeIndex(t, ctx, probe, nodeURL, backupNodeHash, words, "https://example.com/backup")
	seeded := waitRWICount(t, ctx, probe, nodeURL, func(count int) bool { return count > 0 })

	outDir := filepath.Join(workdir, "backups")
	scriptRun(
		t,
		repoRoot,
		"deploy/backup.sh",
		"docker",
		composePath,
		"node",
		nodeVolume,
		outDir,
		"crawler",
		crawlerVolume,
	)
	archive := backupArchive(t, outDir)
	writeVolumeMarker(t, nodeVolume, ".yago-restore-new/blocked")
	failedRestore(
		t,
		repoRoot,
		"docker",
		composePath,
		"node",
		nodeVolume,
		archive,
		"crawler",
		crawlerVolume,
	)
	removeVolumeMarker(t, nodeVolume, ".yago-restore-new")
	nodeURL = backupNodeURL(t, composePath)
	if !waitFor(30*time.Second, func() bool {
		return probe.OK(ctx, nodeURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatal("node did not restart after a rejected restore")
	}
	if retained := waitRWICount(
		t,
		ctx,
		probe,
		nodeURL,
		func(count int) bool { return count == seeded },
	); retained != seeded {
		t.Fatalf("retained RWI count = %d, want %d", retained, seeded)
	}
	requireCrawlerCheckpointSentinel(t, crawlerVolume)

	composeRun(t, composePath, "stop", "crawler", "node")
	wipeVolume(t, nodeVolume)
	wipeVolume(t, crawlerVolume)
	requireVolumePathAbsent(t, crawlerVolume, crawlerCheckpointPath)
	composeRun(t, composePath, "start", "node", "crawler")

	scriptRun(
		t,
		repoRoot,
		"deploy/restore.sh",
		"docker",
		composePath,
		"node",
		nodeVolume,
		archive,
		"crawler",
		crawlerVolume,
	)
	requireCrawlerCheckpointSentinel(t, crawlerVolume)
	nodeURL = backupNodeURL(t, composePath)
	if !waitFor(30*time.Second, func() bool {
		return probe.OK(ctx, nodeURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatal("node never became reachable after the restore")
	}
	restored := waitRWICount(
		t,
		ctx,
		probe,
		nodeURL,
		func(count int) bool { return count == seeded },
	)
	if restored != seeded {
		t.Fatalf("restored RWI count = %d, want the seeded %d", restored, seeded)
	}

	legacyOutDir := filepath.Join(workdir, "legacy-backups")
	scriptRun(
		t,
		repoRoot,
		"deploy/backup.sh",
		"docker",
		composePath,
		"node",
		nodeVolume,
		legacyOutDir,
	)
	legacyArchive := backupArchive(t, legacyOutDir)
	composeRun(t, composePath, "stop", "node")
	wipeVolume(t, nodeVolume)
	composeRun(t, composePath, "start", "node")
	scriptRun(
		t,
		repoRoot,
		"deploy/restore.sh",
		"docker",
		composePath,
		"node",
		nodeVolume,
		legacyArchive,
	)
	nodeURL = backupNodeURL(t, composePath)
	if !waitFor(30*time.Second, func() bool {
		return probe.OK(ctx, nodeURL+"/yacy/query.html?object=rwicount")
	}) {
		t.Fatal("node never became reachable after the legacy restore")
	}
	legacyRestored := waitRWICount(
		t,
		ctx,
		probe,
		nodeURL,
		func(count int) bool { return count == seeded },
	)
	if legacyRestored != seeded {
		t.Fatalf("legacy restored RWI count = %d, want the seeded %d", legacyRestored, seeded)
	}
	requireCrawlerCheckpointSentinel(t, crawlerVolume)
}

type restoreArchiveEntry struct {
	name     string
	body     string
	linkname string
	kind     byte
}

func writeRestoreArchive(t *testing.T, entries ...restoreArchiveEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "restore.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create restore archive: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		header := &tar.Header{
			Name: entry.name, Linkname: entry.linkname, Typeflag: kind, Mode: 0o600,
		}
		if kind == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write restore archive header: %v", err)
		}
		if kind == tar.TypeReg {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatalf("write restore archive body: %v", err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close restore tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close restore gzip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close restore archive: %v", err)
	}

	return path
}

func failedRestore(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()
	command := exec.Command(
		"sh",
		append([]string{filepath.Join(repoRoot, "deploy", "restore.sh")}, args...)...,
	)
	command.Dir = repoRoot
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("restore unexpectedly succeeded: %s", output)
	}

	return string(output)
}

func waitRWICount(
	t *testing.T,
	ctx context.Context,
	probe *httpProbe,
	nodeURL string,
	accept func(int) bool,
) int {
	t.Helper()
	last := -1
	if !waitFor(30*time.Second, func() bool {
		count, ok := peerQueryCount(ctx, probe, nodeURL, backupNodeHash, yagoproto.ObjectRWICount)
		if !ok {
			return false
		}
		last = count
		return accept(count)
	}) {
		t.Fatalf("RWI count never reached the expected value (last=%d)", last)
	}
	return last
}

func writeBackupCompose(t *testing.T, workdir, nodeVolume, crawlerVolume string) string {
	t.Helper()
	compose := fmt.Sprintf(`services:
  node:
    image: %s
    environment:
      YAGO_PEER_BIRTH_DATE: %q
      YAGO_PEER_HASH: %q
      YAGO_PEER_NAME: backup-node
      YAGO_NETWORK_NAME: %q
      YAGO_PEER_ADDR: ":8090"
      YAGO_PUBLIC_ADDR: "off"
      YAGO_ADVERTISE_HOST: 127.0.0.1
      LOG_LEVEL: info
    ports:
      - "127.0.0.1:0:8090"
    volumes:
      - %s:/opt/yago/data
  crawler:
    image: %s
    command: ["tail", "-f", "/dev/null"]
    volumes:
      - %s:/opt/yago/data
volumes:
  %s:
    name: %s
  %s:
    name: %s
`,
		nodeImage(t),
		time.Now().AddDate(0, 0, -5).UTC().Format("20060102"),
		backupNodeHash.String(),
		yagoproto.DefaultNetwork,
		nodeVolume,
		backupVolumeImage,
		crawlerVolume,
		nodeVolume,
		nodeVolume,
		crawlerVolume,
		crawlerVolume,
	)
	path := filepath.Join(workdir, "compose.yaml")
	if err := os.WriteFile(path, []byte(compose), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	return path
}

// composeRun invokes compose exactly like the backup scripts do — no explicit
// project flag, so the project name derives from the compose file's directory
// (made unique per run) and the scripts' own compose calls hit the same project.
func composeRun(t *testing.T, composePath string, args ...string) {
	t.Helper()
	full := append([]string{"compose", "-f", composePath}, args...)
	out, err := exec.Command("docker", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(full, " "), err, out)
	}
}

func composeDown(t *testing.T, composePath string) {
	t.Helper()
	out, err := exec.Command(
		"docker", "compose", "-f", composePath,
		"down", "-v", "--remove-orphans",
	).CombinedOutput()
	if err != nil {
		t.Logf("compose down: %v\n%s", err, out)
	}
}

// backupNodeURL resolves the node's current published peer port; stop/start
// keeps the container, but the port is re-resolved after every lifecycle step
// anyway so the test never depends on that.
func backupNodeURL(t *testing.T, composePath string) string {
	t.Helper()
	var out []byte
	if !waitFor(30*time.Second, func() bool {
		var err error
		out, err = exec.Command(
			"docker", "compose", "-f", composePath,
			"port", "node", "8090",
		).CombinedOutput()
		return err == nil && strings.Contains(string(out), ":")
	}) {
		t.Fatalf("resolve node port: %s", out)
	}
	return "http://" + strings.TrimSpace(string(out))
}

func scriptRun(t *testing.T, repoRoot, script string, args ...string) {
	t.Helper()
	command := exec.Command("sh", append([]string{filepath.Join(repoRoot, script)}, args...)...)
	command.Dir = repoRoot
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", script, strings.Join(args, " "), err, out)
	}
	t.Logf("%s: %s", script, lastLine(out))
}

func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines[len(lines)-1]
}

func backupArchive(t *testing.T, outDir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(outDir, "yago-backup-*.tar.gz"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("backup archive not found in %s: %v %v", outDir, matches, err)
	}
	return matches[0]
}

func writeCrawlerCheckpointSentinel(t *testing.T, composePath string) {
	t.Helper()
	composeRun(
		t,
		composePath,
		"exec",
		"-T",
		"crawler",
		"sh",
		"-c",
		"mkdir -p /opt/yago/data/crawler && printf '%s' '"+
			crawlerCheckpointSentinel+"' > /opt/yago/data/"+crawlerCheckpointPath,
	)
}

func requireCrawlerCheckpointSentinel(t *testing.T, volume string) {
	t.Helper()
	out, err := exec.Command(
		"docker",
		"run",
		"--rm",
		"-v",
		volume+":/data:ro",
		backupVolumeImage,
		"cat",
		"/data/"+crawlerCheckpointPath,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("read crawler checkpoint sentinel: %v\n%s", err, out)
	}
	if string(out) != crawlerCheckpointSentinel {
		t.Fatalf("crawler checkpoint sentinel = %q, want %q", out, crawlerCheckpointSentinel)
	}
}

func requireVolumePathAbsent(t *testing.T, volume, path string) {
	t.Helper()
	out, err := exec.Command(
		"docker",
		"run",
		"--rm",
		"-v",
		volume+":/data:ro",
		backupVolumeImage,
		"test",
		"!",
		"-e",
		"/data/"+path,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("volume path %s survived wipe: %v\n%s", path, err, out)
	}
}

func writeVolumeMarker(t *testing.T, volume string, path string) {
	t.Helper()
	out, err := exec.Command(
		"docker",
		"run",
		"--rm",
		"-v",
		volume+":/data",
		backupVolumeImage,
		"sh",
		"-c",
		"mkdir -p \"/data/$(dirname \"$1\")\" && : > \"/data/$1\"",
		"marker",
		path,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("write volume marker: %v\n%s", err, out)
	}
}

func removeVolumeMarker(t *testing.T, volume string, path string) {
	t.Helper()
	out, err := exec.Command(
		"docker",
		"run",
		"--rm",
		"-v",
		volume+":/data",
		backupVolumeImage,
		"rm",
		"-rf",
		"/data/"+path,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("remove volume marker: %v\n%s", err, out)
	}
}

// wipeVolume destroys the data volume's contents so a fictitious restore cannot
// pass on leftovers; restore.sh wipes again before unpacking, by design.
func wipeVolume(t *testing.T, volume string) {
	t.Helper()
	out, err := exec.Command(
		"docker", "run", "--rm",
		"-v", volume+":/data",
		backupVolumeImage, "sh", "-c", "rm -rf /data/* /data/..?* /data/.[!.]*",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("wipe volume: %v\n%s", err, out)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "deploy", "backup.sh")); err != nil {
		t.Fatalf("deploy/backup.sh not found from %s: %v", root, err)
	}
	return root
}
