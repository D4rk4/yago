package crawlorder

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunProgressReporterReportsOnTickThenStopsOnStop(t *testing.T) {
	var reports atomic.Int64
	fired := make(chan struct{}, 1)
	reporter := newRunProgressReporter()
	reporter.reportWith(func() {
		reports.Add(1)
		select {
		case fired <- struct{}{}:
		default:
		}
	})

	done := make(chan struct{})
	go func() {
		reporter.run(context.Background(), time.Millisecond)
		close(done)
	}()

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("periodic report did not fire")
	}

	reporter.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reporter did not return after Stop")
	}
	if reports.Load() == 0 {
		t.Fatal("expected at least one report")
	}
}

func TestRunProgressReporterStopsOnContextCancel(t *testing.T) {
	reporter := newRunProgressReporter()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		reporter.run(ctx, time.Hour)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reporter did not return on context cancel")
	}
}

func TestRunProgressReporterSkipsReportAfterStop(t *testing.T) {
	var reports atomic.Int64
	reporter := newRunProgressReporter()

	reporter.reportRunning() // no closure attached yet: must be a no-op
	if reports.Load() != 0 {
		t.Fatalf("reports before reportWith = %d, want 0", reports.Load())
	}

	reporter.reportWith(func() { reports.Add(1) })
	reporter.reportRunning()
	if reports.Load() != 1 {
		t.Fatalf("reports before stop = %d, want 1", reports.Load())
	}

	reporter.Stop()
	reporter.Stop() // idempotent
	reporter.reportRunning()
	if reports.Load() != 1 {
		t.Fatalf(
			"reports after stop = %d, want the finished run to suppress reporting",
			reports.Load(),
		)
	}
}
