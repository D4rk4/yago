//go:build e2e

package e2e

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
)

func TestSystemdBackupRestorePreservesCrawlBrokerState(t *testing.T) {
	repoRoot := repositoryRoot(t)
	temporaryRoot := t.TempDir()
	dataDirectory := filepath.Join(temporaryRoot, "data")
	outputDirectory := filepath.Join(temporaryRoot, "backups")
	commandDirectory := filepath.Join(temporaryRoot, "bin")
	commandLog := filepath.Join(temporaryRoot, "commands.log")
	createBackupTestDirectories(t, dataDirectory, commandDirectory)
	writeBackupTestExecutable(t, commandDirectory, "systemctl", `#!/bin/sh
printf '%s\n' "$*" >> "$YAGO_BACKUP_COMMAND_LOG"
case "$1" in
is-active|stop|start) exit 0 ;;
*) exit 2 ;;
esac
`)

	statePath := filepath.Join(dataDirectory, "crawlbroker.db")
	profiles := []string{"first", "second", "third"}
	identity := backupTerminalIdentity()
	progress := backupTerminalProgress()
	writeBackupBrokerState(t, statePath, profiles, identity, progress)

	output, err := executeBackupScript(
		t,
		repoRoot,
		commandDirectory,
		commandLog,
		nil,
		"systemd",
		"yago-node.service",
		dataDirectory,
		outputDirectory,
		"yago-crawler.service",
	)
	if err != nil {
		t.Fatalf("backup crawl state: %v\n%s", err, output)
	}
	archive := backupArchive(t, outputDirectory)
	if err := os.RemoveAll(dataDirectory); err != nil {
		t.Fatalf("remove crawl state: %v", err)
	}
	if err := os.Mkdir(dataDirectory, 0o700); err != nil {
		t.Fatalf("recreate crawl state directory: %v", err)
	}

	output, err = executeRestoreScript(
		t,
		repoRoot,
		commandDirectory,
		commandLog,
		"systemd",
		"yago-node.service",
		dataDirectory,
		archive,
		"yago-crawler.service",
	)
	if err != nil {
		t.Fatalf("restore crawl state: %v\n%s", err, output)
	}
	requireBackupCrawlState(t, statePath, profiles, identity, progress)
}

func TestSystemdBackupLeavesInitiallyInactiveCrawlerStopped(t *testing.T) {
	repoRoot := repositoryRoot(t)
	temporaryRoot := t.TempDir()
	dataDirectory := filepath.Join(temporaryRoot, "data")
	outputDirectory := filepath.Join(temporaryRoot, "backups")
	commandDirectory := filepath.Join(temporaryRoot, "bin")
	commandLog := filepath.Join(temporaryRoot, "commands.log")
	createBackupTestDirectories(t, dataDirectory, commandDirectory)
	writeBackupTestExecutable(t, commandDirectory, "systemctl", `#!/bin/sh
printf '%s\n' "$*" >> "$YAGO_BACKUP_COMMAND_LOG"
case "$1" in
is-active) [ "$3" = "yago-node.service" ] ;;
stop|start) exit 0 ;;
*) exit 2 ;;
esac
`)

	output, err := executeBackupScript(
		t,
		repoRoot,
		commandDirectory,
		commandLog,
		nil,
		"systemd",
		"yago-node.service",
		dataDirectory,
		outputDirectory,
		"yago-crawler.service",
	)
	if err != nil {
		t.Fatalf("backup failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "backup written:") {
		t.Fatalf("backup output = %q", output)
	}
	requireBackupCommandSequence(t, commandLog, []string{
		"is-active --quiet yago-node.service",
		"is-active --quiet yago-crawler.service",
		"stop yago-node.service",
		"start yago-node.service",
	})
	requireBackupCommandAbsent(t, commandLog, "stop yago-crawler.service")
	requireBackupCommandAbsent(t, commandLog, "start yago-crawler.service")
}

func TestSystemdBackupPreservesArchiveFailureWhenRestartFails(t *testing.T) {
	repoRoot := repositoryRoot(t)
	temporaryRoot := t.TempDir()
	dataDirectory := filepath.Join(temporaryRoot, "data")
	outputDirectory := filepath.Join(temporaryRoot, "backups")
	commandDirectory := filepath.Join(temporaryRoot, "bin")
	commandLog := filepath.Join(temporaryRoot, "commands.log")
	createBackupTestDirectories(t, dataDirectory, commandDirectory)
	writeBackupTestExecutable(t, commandDirectory, "systemctl", `#!/bin/sh
printf '%s\n' "$*" >> "$YAGO_BACKUP_COMMAND_LOG"
case "$1" in
is-active|stop) exit 0 ;;
start) [ "$2" != "yago-node.service" ] ;;
*) exit 2 ;;
esac
`)
	writeBackupTestExecutable(t, commandDirectory, "tar", `#!/bin/sh
exit 42
`)

	output, err := executeBackupScript(
		t,
		repoRoot,
		commandDirectory,
		commandLog,
		nil,
		"systemd",
		"yago-node.service",
		dataDirectory,
		outputDirectory,
		"yago-crawler.service",
	)
	requireBackupExitCode(t, err, 42)
	if !strings.Contains(output, "failed to restart systemd unit: yago-node.service") ||
		strings.Contains(output, "backup written:") {
		t.Fatalf("backup failure output = %q", output)
	}
	requireBackupCommandSequence(t, commandLog, []string{
		"stop yago-crawler.service",
		"stop yago-node.service",
		"start yago-node.service",
	})
	requireBackupCommandAbsent(t, commandLog, "start yago-crawler.service")
}

func TestDockerDualVolumeBackupLeavesInitiallyInactiveCrawlerStopped(t *testing.T) {
	repoRoot := repositoryRoot(t)
	temporaryRoot := t.TempDir()
	outputDirectory := filepath.Join(temporaryRoot, "backups")
	commandDirectory := filepath.Join(temporaryRoot, "bin")
	commandLog := filepath.Join(temporaryRoot, "commands.log")
	createBackupTestDirectories(t, outputDirectory, commandDirectory)
	writeBackupTestExecutable(t, commandDirectory, "docker", dockerBackupTestCommand)

	output, err := executeBackupScript(
		t,
		repoRoot,
		commandDirectory,
		commandLog,
		nil,
		"docker",
		"compose.yaml",
		"node",
		"node-volume",
		outputDirectory,
		"crawler",
		"crawler-volume",
	)
	if err != nil {
		t.Fatalf("backup failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "backup written:") {
		t.Fatalf("backup output = %q", output)
	}
	requireBackupCommandSequence(t, commandLog, []string{
		"compose -f compose.yaml ps --status running -q node",
		"compose -f compose.yaml ps --status running -q crawler",
		"compose -f compose.yaml stop node",
		"run --rm -v node-volume:/node:ro -v crawler-volume:/crawler:ro",
		"compose -f compose.yaml start node",
	})
	requireBackupCommandAbsent(t, commandLog, "stop crawler")
	requireBackupCommandAbsent(t, commandLog, "start crawler")
}

func TestDockerDualVolumeBackupSurfacesCrawlerRestartFailure(t *testing.T) {
	repoRoot := repositoryRoot(t)
	temporaryRoot := t.TempDir()
	outputDirectory := filepath.Join(temporaryRoot, "backups")
	commandDirectory := filepath.Join(temporaryRoot, "bin")
	commandLog := filepath.Join(temporaryRoot, "commands.log")
	createBackupTestDirectories(t, outputDirectory, commandDirectory)
	writeBackupTestExecutable(t, commandDirectory, "docker", dockerBackupTestCommand)

	output, err := executeBackupScript(
		t,
		repoRoot,
		commandDirectory,
		commandLog,
		[]string{"YAGO_BACKUP_CRAWLER_RUNNING=1", "YAGO_BACKUP_FAIL_CRAWLER_START=1"},
		"docker",
		"compose.yaml",
		"node",
		"node-volume",
		outputDirectory,
		"crawler",
		"crawler-volume",
	)
	requireBackupExitCode(t, err, 1)
	if !strings.Contains(output, "failed to restart Docker service: crawler") ||
		strings.Contains(output, "backup written:") {
		t.Fatalf("backup failure output = %q", output)
	}
	requireBackupCommandSequence(t, commandLog, []string{
		"compose -f compose.yaml stop crawler",
		"compose -f compose.yaml stop node",
		"run --rm -v node-volume:/node:ro -v crawler-volume:/crawler:ro",
		"compose -f compose.yaml start node",
		"compose -f compose.yaml start crawler",
	})
}

func TestDockerDualVolumeBackupPreservesArchiveFailureWhenRestartFails(t *testing.T) {
	repoRoot := repositoryRoot(t)
	temporaryRoot := t.TempDir()
	outputDirectory := filepath.Join(temporaryRoot, "backups")
	commandDirectory := filepath.Join(temporaryRoot, "bin")
	commandLog := filepath.Join(temporaryRoot, "commands.log")
	createBackupTestDirectories(t, outputDirectory, commandDirectory)
	writeBackupTestExecutable(t, commandDirectory, "docker", dockerBackupTestCommand)

	output, err := executeBackupScript(
		t,
		repoRoot,
		commandDirectory,
		commandLog,
		[]string{
			"YAGO_BACKUP_CRAWLER_RUNNING=1",
			"YAGO_BACKUP_FAIL_ARCHIVE=1",
			"YAGO_BACKUP_FAIL_NODE_START=1",
		},
		"docker",
		"compose.yaml",
		"node",
		"node-volume",
		outputDirectory,
		"crawler",
		"crawler-volume",
	)
	requireBackupExitCode(t, err, 42)
	if !strings.Contains(output, "failed to restart Docker service: node") ||
		strings.Contains(output, "backup written:") {
		t.Fatalf("backup failure output = %q", output)
	}
	requireBackupCommandSequence(t, commandLog, []string{
		"compose -f compose.yaml stop crawler",
		"compose -f compose.yaml stop node",
		"run --rm -v node-volume:/node:ro -v crawler-volume:/crawler:ro",
		"compose -f compose.yaml start node",
	})
	requireBackupCommandAbsent(t, commandLog, "start crawler")
}

const dockerBackupTestCommand = `#!/bin/sh
printf '%s\n' "$*" >> "$YAGO_BACKUP_COMMAND_LOG"
if [ "$1" = "run" ]; then
	[ -z "${YAGO_BACKUP_FAIL_ARCHIVE:-}" ] || exit 42
	exit 0
fi
action="$4"
service="${8:-${5:-}}"
case "$action" in
ps)
	if [ "$service" = "node" ]; then
		printf '%s\n' node-container
	elif [ -n "${YAGO_BACKUP_CRAWLER_RUNNING:-}" ]; then
		printf '%s\n' crawler-container
	fi
	;;
stop) exit 0 ;;
start)
	service="$5"
	if [ "$service" = "node" ] && [ -n "${YAGO_BACKUP_FAIL_NODE_START:-}" ]; then
		exit 1
	fi
	if [ "$service" = "crawler" ] && [ -n "${YAGO_BACKUP_FAIL_CRAWLER_START:-}" ]; then
		exit 1
	fi
	;;
*) exit 2 ;;
esac
`

func createBackupTestDirectories(t *testing.T, directories ...string) {
	t.Helper()
	for _, directory := range directories {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("create backup test directory: %v", err)
		}
	}
}

func writeBackupTestExecutable(t *testing.T, directory, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0o700); err != nil {
		t.Fatalf("write backup test command: %v", err)
	}
}

func executeBackupScript(
	t *testing.T,
	repoRoot string,
	commandDirectory string,
	commandLog string,
	extraEnvironment []string,
	arguments ...string,
) (string, error) {
	t.Helper()
	command := exec.Command(
		"sh",
		append([]string{filepath.Join(repoRoot, "deploy", "backup.sh")}, arguments...)...,
	)
	command.Dir = repoRoot
	command.Env = append(
		append(os.Environ(), "PATH="+commandDirectory+":"+os.Getenv("PATH")),
		append([]string{"YAGO_BACKUP_COMMAND_LOG=" + commandLog}, extraEnvironment...)...,
	)
	output, err := command.CombinedOutput()
	return string(output), err
}

func executeRestoreScript(
	t *testing.T,
	repoRoot string,
	commandDirectory string,
	commandLog string,
	arguments ...string,
) (string, error) {
	t.Helper()
	command := exec.Command(
		"sh",
		append([]string{filepath.Join(repoRoot, "deploy", "restore.sh")}, arguments...)...,
	)
	command.Dir = repoRoot
	command.Env = append(
		os.Environ(),
		"PATH="+commandDirectory+":"+os.Getenv("PATH"),
		"YAGO_BACKUP_COMMAND_LOG="+commandLog,
	)
	output, err := command.CombinedOutput()
	return string(output), err
}

func writeBackupBrokerState(
	t *testing.T,
	statePath string,
	profiles []string,
	identity []byte,
	progress yagocrawlcontract.CrawlRunProgress,
) {
	t.Helper()
	ctx := context.Background()
	storage, err := boltvault.OpenWithLockTimeout(statePath, time.Second)
	if err != nil {
		t.Fatalf("open backup crawl state: %v", err)
	}
	runs, err := crawlruns.Open(ctx, storage, 8)
	if err != nil {
		_ = storage.Close()
		t.Fatalf("open backup terminal runs: %v", err)
	}
	broker, err := crawlbroker.Open(
		crawlbroker.Config{ListenAddr: "127.0.0.1:0"},
		storage,
		runs,
	)
	if err != nil {
		_ = storage.Close()
		t.Fatalf("open backup crawl broker: %v", err)
	}
	for _, profile := range profiles {
		order := yagocrawlcontract.CrawlOrder{
			Provenance: []byte(profile),
			Profile: yagocrawlcontract.NewCrawlProfile(
				yagocrawlcontract.CrawlProfile{Name: profile},
			),
			Requests: []yagocrawlcontract.CrawlRequest{{
				URL: "https://example.org/" + profile,
			}},
		}
		if err := broker.Orders.Publish(ctx, order); err != nil {
			broker.Close()
			_ = storage.Close()
			t.Fatalf("publish backup order %q: %v", profile, err)
		}
	}
	if err := runs.RecordTerminal(ctx, identity, progress); err != nil {
		broker.Close()
		_ = storage.Close()
		t.Fatalf("record backup terminal progress: %v", err)
	}
	broker.Close()
	if err := storage.Close(); err != nil {
		t.Fatalf("close backup crawl state: %v", err)
	}
}

func requireBackupCrawlState(
	t *testing.T,
	statePath string,
	profiles []string,
	identity []byte,
	progress yagocrawlcontract.CrawlRunProgress,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	storage, err := boltvault.OpenWithLockTimeout(statePath, time.Second)
	if err != nil {
		t.Fatalf("reopen restored crawl state: %v", err)
	}
	defer func() { _ = storage.Close() }()
	runs, err := crawlruns.Open(ctx, storage, 8)
	if err != nil {
		t.Fatalf("open restored terminal runs: %v", err)
	}
	address := backupBrokerAddress(t)
	broker, err := crawlbroker.Open(
		crawlbroker.Config{ListenAddr: address},
		storage,
		runs,
	)
	if err != nil {
		t.Fatalf("open restored crawl broker: %v", err)
	}
	defer broker.Close()
	connection, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("connect restored crawl broker: %v", err)
	}
	defer func() { _ = connection.Close() }()
	stream, err := crawlrpc.NewCrawlExchangeClient(connection).StreamOrders(
		ctx,
		&crawlrpc.WorkerRegistration{
			WorkerId:        "backup-restore-worker",
			WorkerSessionId: "backup-restore-session",
		},
	)
	if err != nil {
		t.Fatalf("stream restored crawl orders: %v", err)
	}
	for index, profile := range profiles {
		message, receiveErr := stream.Recv()
		if receiveErr != nil {
			t.Fatalf("receive restored order %d: %v", index, receiveErr)
		}
		order, decodeErr := yagocrawlcontract.UnmarshalCrawlOrder(message.GetOrderJson())
		if decodeErr != nil {
			t.Fatalf("decode restored order %d: %v", index, decodeErr)
		}
		if order.Profile.Name != profile {
			t.Fatalf("restored order %d profile = %q, want %q", index, order.Profile.Name, profile)
		}
	}
	recent := runs.Recent()
	if len(recent) != 1 || recent[0].RunID != progress.RunID || recent[0].Tally != progress.Tally {
		t.Fatalf("restored terminal runs = %+v", recent)
	}
	if err := runs.ConfirmTerminalDelivery(ctx, identity); err != nil {
		t.Fatalf("confirm restored terminal progress: %v", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("close restored crawl connection: %v", err)
	}
	broker.Close()
	if err := storage.Close(); err != nil {
		t.Fatalf("close restored crawl state: %v", err)
	}
}

func backupBrokerAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve backup broker address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release backup broker address: %v", err)
	}

	return address
}

func backupTerminalIdentity() []byte {
	identity := make([]byte, 32)
	identity[0] = 1
	identity[31] = 2

	return identity
}

func backupTerminalProgress() yagocrawlcontract.CrawlRunProgress {
	return yagocrawlcontract.CrawlRunProgress{
		RunID:         "restored-terminal-run",
		WorkerID:      "backup-worker",
		ProfileHandle: "backup-profile",
		ProfileName:   "backup terminal",
		State:         yagocrawlcontract.CrawlRunFinished,
		Tally: yagocrawlcontract.CrawlRunTally{
			Fetched: 3,
			Indexed: 2,
			Failed:  1,
		},
	}
}

func requireBackupExitCode(t *testing.T, err error, expected int) {
	t.Helper()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != expected {
		t.Fatalf("backup exit error = %v, want code %d", err, expected)
	}
}

func requireBackupCommandSequence(t *testing.T, commandLog string, expected []string) {
	t.Helper()
	data, err := os.ReadFile(commandLog)
	if err != nil {
		t.Fatalf("read backup command log: %v", err)
	}
	remaining := string(data)
	for _, command := range expected {
		position := strings.Index(remaining, command)
		if position < 0 {
			t.Fatalf("backup command %q missing or out of order in %q", command, data)
		}
		remaining = remaining[position+len(command):]
	}
}

func requireBackupCommandAbsent(t *testing.T, commandLog, command string) {
	t.Helper()
	data, err := os.ReadFile(commandLog)
	if err != nil {
		t.Fatalf("read backup command log: %v", err)
	}
	if strings.Contains(string(data), command) {
		t.Fatalf("unexpected backup command %q in %q", command, data)
	}
}
