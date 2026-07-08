package visitintake_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacyvisitcrawl/internal/visitintake"
)

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

func TestBoundedPlacementRecordsSuccess(t *testing.T) {
	metrics := &recordingMetrics{}
	placement := visitintake.NewBoundedPlacement(
		func(_ context.Context, _ yacycrawlcontract.CrawlOrder) error { return nil },
		metrics, time.Second, 1,
	)
	placement.Attempt(yacycrawlcontract.CrawlOrder{OrderID: "o1"})
	waitFor(t, func() bool { return metrics.placedCount() == 1 })
	if unplaced := metrics.unplacedCount(); unplaced != 0 {
		t.Fatalf("unplaced = %d, want 0", unplaced)
	}
}

func TestBoundedPlacementRecordsFailure(t *testing.T) {
	metrics := &recordingMetrics{}
	placement := visitintake.NewBoundedPlacement(
		func(_ context.Context, _ yacycrawlcontract.CrawlOrder) error { return errors.New("broker down") },
		metrics,
		time.Second,
		1,
	)
	placement.Attempt(yacycrawlcontract.CrawlOrder{OrderID: "o1"})
	waitFor(t, func() bool { return metrics.unplacedCount() == 1 })
}

func TestBoundedPlacementSaturationSkipsWithoutBlocking(t *testing.T) {
	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(1)

	metrics := &recordingMetrics{}
	placement := visitintake.NewBoundedPlacement(
		func(_ context.Context, _ yacycrawlcontract.CrawlOrder) error {
			started.Done()
			<-release
			return nil
		},
		metrics, time.Second, 1,
	)

	placement.Attempt(yacycrawlcontract.CrawlOrder{OrderID: "o1"})
	started.Wait()

	done := make(chan struct{})
	go func() {
		placement.Attempt(yacycrawlcontract.CrawlOrder{OrderID: "o2"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second attempt blocked instead of skipping")
	}

	waitFor(t, func() bool { return metrics.unplacedCount() == 1 })
	close(release)
	waitFor(t, func() bool { return metrics.placedCount() == 1 })
}
