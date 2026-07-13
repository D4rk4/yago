package crawlorder

import (
	"context"
	"fmt"
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
		reporter.run(context.Background(), time.Millisecond, time.Millisecond)
		close(done)
	}()

	for reports.Load() < 2 {
		select {
		case <-fired:
		case <-time.After(2 * time.Second):
			t.Fatal("periodic report did not fire")
		}
	}

	reporter.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reporter did not return after Stop")
	}
	if reports.Load() < 2 {
		t.Fatal("expected the phased and periodic reports")
	}
}

func TestRunProgressReporterStopsOnContextCancel(t *testing.T) {
	reporter := newRunProgressReporter()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		reporter.run(ctx, time.Hour, time.Hour)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reporter did not return on context cancel")
	}
}

func TestRunProgressReporterStopsBeforeInitialPhase(t *testing.T) {
	reporter := newRunProgressReporter()
	done := make(chan struct{})
	go func() {
		reporter.run(context.Background(), time.Hour, time.Hour)
		close(done)
	}()
	reporter.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reporter did not stop before its initial phase")
	}
}

func TestRunProgressReporterContextStopsAfterInitialPhase(t *testing.T) {
	reporter := newRunProgressReporter()
	fired := make(chan struct{}, 1)
	reporter.reportWith(func() { fired <- struct{}{} })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		reporter.run(ctx, time.Hour, 0)
		close(done)
	}()
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("initial phased report did not fire")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reporter did not stop after initial phase cancellation")
	}
}

func TestProgressReportPhaseDistributesConcurrentRuns(t *testing.T) {
	const buckets = 10
	interval := 5 * time.Second
	seen := make(map[int]struct{}, buckets)
	for run := range 40 {
		phase := progressReportPhase([]byte(fmt.Sprintf("run-%d", run)), interval)
		if phase < 0 || phase >= interval {
			t.Fatalf("phase = %v outside interval", phase)
		}
		seen[int(phase/(interval/buckets))] = struct{}{}
	}
	if len(seen) < 8 {
		t.Fatalf("40 runs reached only %d/%d progress buckets", len(seen), buckets)
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
