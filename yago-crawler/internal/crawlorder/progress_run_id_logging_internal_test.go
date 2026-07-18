package crawlorder

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type recordedProgressLog struct {
	message string
	runID   string
}

type progressLogRecorder struct {
	records chan recordedProgressLog
}

func (recorder progressLogRecorder) Enabled(context.Context, slog.Level) bool {
	return true
}

func (recorder progressLogRecorder) Handle(_ context.Context, record slog.Record) error {
	entry := recordedProgressLog{message: record.Message}
	record.Attrs(func(attribute slog.Attr) bool {
		if attribute.Key == "runId" {
			entry.runID = attribute.Value.String()
		}

		return true
	})
	select {
	case recorder.records <- entry:
	default:
	}

	return nil
}

func (recorder progressLogRecorder) WithAttrs([]slog.Attr) slog.Handler {
	return recorder
}

func (recorder progressLogRecorder) WithGroup(string) slog.Handler {
	return recorder
}

func TestDroppedProgressReportLogsHexRunID(t *testing.T) {
	records := installProgressLogRecorder(t)
	gate := make(chan struct{})
	started := make(chan *crawlrpc.CrawlProgressReport, 1)
	client := &progressDeliveryClient{gate: gate, started: started}
	policy := testProgressDeliveryPolicy()
	policy.capacity = 1
	queue := newProgressDeliveryQueue(client, "worker", policy)
	queue.enqueue(t.Context(), RunReport{
		Provenance: []byte("occupied"),
		State:      yagocrawlcontract.CrawlRunRunning,
	})
	waitProgressCall(t, started)
	queue.enqueue(t.Context(), RunReport{
		Provenance: []byte{0x00, 0xff, 0x41},
		State:      yagocrawlcontract.CrawlRunRunning,
	})
	assertProgressLogRunID(t, records, msgProgressReportDropped, "00ff41")
	close(gate)
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close saturated progress queue: %v", err)
	}
}

func TestFailedProgressReportLogsHexRunID(t *testing.T) {
	records := installProgressLogRecorder(t)
	started := make(chan *crawlrpc.CrawlProgressReport, 1)
	client := &progressDeliveryClient{
		errors:  []error{errors.New("unavailable")},
		started: started,
	}
	queue := newProgressDeliveryQueue(client, "worker", testProgressDeliveryPolicy())
	queue.enqueue(t.Context(), RunReport{
		Provenance: []byte{0x80, 0x00, 0xff},
		State:      yagocrawlcontract.CrawlRunRunning,
	})
	waitProgressCall(t, started)
	assertProgressLogRunID(t, records, msgProgressReportFailed, "8000ff")
	if err := queue.close(t.Context()); err != nil {
		t.Fatalf("close failed progress queue: %v", err)
	}
}

func installProgressLogRecorder(t *testing.T) <-chan recordedProgressLog {
	t.Helper()
	previous := slog.Default()
	records := make(chan recordedProgressLog, 8)
	slog.SetDefault(slog.New(progressLogRecorder{records: records}))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return records
}

func assertProgressLogRunID(
	t *testing.T,
	records <-chan recordedProgressLog,
	message string,
	want string,
) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case record := <-records:
			if record.message != message {
				continue
			}
			if record.runID != want {
				t.Fatalf("progress log run id = %q, want %q", record.runID, want)
			}

			return
		case <-timer.C:
			t.Fatalf("progress log %q was not recorded", message)
		}
	}
}
