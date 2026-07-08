//go:build e2e

package e2e

import (
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

const backupNodeHash = yagomodel.Hash("DEFGHIJKLMNO")

// TestBackupRestoreRoundTripPreservesIndex is the OPS-03 acceptance: with a
// small seeded index, deploy/backup.sh archives the stopped node's data volume,
// the volume is wiped, deploy/restore.sh restores the archive, and the restarted
// node still serves the seeded postings. The scripts themselves run — the test
// drives the same docker mode an operator uses, over a throwaway compose
// project on the /opt/yago data layout.
func TestBackupRestoreRoundTripPreservesIndex(t *testing.T) {
	ctx := context.Background()
	probe := newHTTPProbe(t)
	repoRoot := repositoryRoot(t)
	project := fmt.Sprintf("yagobackup%d", os.Getpid())
	workdir := filepath.Join(t.TempDir(), project)
	if err := os.MkdirAll(workdir, 0o700); err != nil {
		t.Fatalf("workdir: %v", err)
	}
	volume := project + "-data"
	composePath := writeBackupCompose(t, workdir, volume)
	defer composeDown(t, composePath)

	composeRun(t, composePath, "up", "-d")
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
	scriptRun(t, repoRoot, "deploy/backup.sh", "docker", composePath, "node", volume, outDir)
	archive := backupArchive(t, outDir)

	composeRun(t, composePath, "stop", "node")
	wipeVolume(t, volume)

	scriptRun(t, repoRoot, "deploy/restore.sh", "docker", composePath, "node", volume, archive)
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

// writeBackupCompose renders the throwaway single-node compose project the
// backup scripts operate on: the e2e node image, the named data volume on the
// /opt/yago layout, and a loopback-published random peer port.
func writeBackupCompose(t *testing.T, workdir, volume string) string {
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
volumes:
  %s:
    name: %s
`,
		nodeImage(t),
		time.Now().AddDate(0, 0, -5).UTC().Format("20060102"),
		backupNodeHash.String(),
		yagoproto.DefaultNetwork,
		volume, volume, volume,
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

// wipeVolume destroys the data volume's contents so a fictitious restore cannot
// pass on leftovers; restore.sh wipes again before unpacking, by design.
func wipeVolume(t *testing.T, volume string) {
	t.Helper()
	out, err := exec.Command(
		"docker", "run", "--rm",
		"-v", volume+":/data",
		"alpine", "sh", "-c", "rm -rf /data/* /data/..?* /data/.[!.]*",
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
