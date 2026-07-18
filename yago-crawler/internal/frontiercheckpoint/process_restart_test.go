package frontiercheckpoint_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	checkpointProcessChild = "YAGO_CHECKPOINT_PROCESS_CHILD"
	checkpointProcessPath  = "YAGO_CHECKPOINT_PROCESS_PATH"
)

func TestCheckpointSurvivesProcessExitWithoutClose(t *testing.T) {
	if os.Getenv(checkpointProcessChild) == "1" {
		writeCheckpointBeforeProcessExit(os.Getenv(checkpointProcessPath))
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "frontier-v1.db")
	command := exec.CommandContext(
		t.Context(),
		"/proc/self/exe",
		"-test.run=^TestCheckpointSurvivesProcessExitWithoutClose$",
	)
	command.Env = append(
		os.Environ(),
		checkpointProcessChild+"=1",
		checkpointProcessPath+"="+path,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("checkpoint child: %v: %s", err, output)
	}
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("reopen checkpoint after process exit: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	snapshot, err := checkpoint.Load(context.Background(), []byte("process-restart"))
	if err != nil {
		t.Fatalf("load process checkpoint: %v", err)
	}
	if !snapshot.Seeding || snapshot.Counters.Pending != 1 ||
		len(snapshot.Outstanding) != 1 ||
		snapshot.Outstanding[0].ObservationID != "process-observation" {
		t.Fatalf("process checkpoint snapshot = %+v", snapshot)
	}
}

func writeCheckpointBeforeProcessExit(path string) {
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	provenance := []byte("process-restart")
	if err := checkpoint.Begin(
		ctx,
		provenance,
		[]byte("process-order"),
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		panic(err)
	}
	admitted, err := checkpoint.Admit(ctx, provenance, []frontiercheckpoint.Page{{
		URL:           "https://example.org/process",
		Host:          "example.org",
		ProfileHandle: "process-profile",
		ObservationID: "process-observation",
		ObservedAt:    time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC),
		Index:         true,
	}})
	if err != nil || admitted != 1 {
		panic("checkpoint admission failed")
	}
}
