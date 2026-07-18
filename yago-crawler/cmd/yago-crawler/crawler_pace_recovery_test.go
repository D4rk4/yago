package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
)

type scriptedHostPaceLedger struct {
	states   map[string]crawlpace.HostState
	err      error
	capacity int
	ctxErr   error
}

func (ledger *scriptedHostPaceLedger) HostPaces(
	ctx context.Context,
	capacity int,
) (map[string]crawlpace.HostState, error) {
	ledger.capacity = capacity
	ledger.ctxErr = ctx.Err()

	return ledger.states, ledger.err
}

type recordingHostPace struct {
	capacity int
	restored map[string]crawlpace.HostState
}

func (*recordingHostPace) SnapshotHost(string) crawlpace.HostState {
	return crawlpace.HostState{}
}

func (pace *recordingHostPace) RestoreHost(host string, state crawlpace.HostState) {
	pace.restored[host] = state
}

func (pace *recordingHostPace) Capacity() int {
	return pace.capacity
}

func TestRestoreCrawlerHostPacesUsesDurableLedger(t *testing.T) {
	state := crawlpace.HostState{
		NextDueAt:       time.Date(2026, 7, 16, 16, 0, 0, 0, time.UTC),
		BackoffUntil:    time.Date(2026, 7, 16, 16, 10, 0, 0, time.UTC),
		BackoffPenalty:  10 * time.Minute,
		BackoffFailures: 2,
		Generation:      7,
	}
	ledger := &scriptedHostPaceLedger{states: map[string]crawlpace.HostState{
		"busy.example": state,
	}}
	pace := &recordingHostPace{capacity: 17, restored: make(map[string]crawlpace.HostState)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := restoreCrawlerHostPaces(ctx, ledger, pace); err != nil {
		t.Fatalf("restore host pace: %v", err)
	}
	if ledger.capacity != 17 || ledger.ctxErr != nil {
		t.Fatalf("ledger call capacity/context = %d, %v", ledger.capacity, ledger.ctxErr)
	}
	if pace.restored["busy.example"] != state {
		t.Fatalf("restored host state = %+v, want %+v", pace.restored, state)
	}
}

func TestRestoreCrawlerHostPacesPropagatesLedgerFailure(t *testing.T) {
	sentinel := errors.New("pace ledger failed")
	ledger := &scriptedHostPaceLedger{err: sentinel}
	pace := &recordingHostPace{capacity: 1, restored: make(map[string]crawlpace.HostState)}
	if err := restoreCrawlerHostPaces(
		context.Background(),
		ledger,
		pace,
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("restore error = %v, want %v", err, sentinel)
	}
}
