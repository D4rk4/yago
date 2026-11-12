package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
)

func TestCrawlerProgressLogSeparatesRatesAndBacklogs(t *testing.T) {
	previousLogger := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	activity := newCrawlerActivity(time.Now())
	activity.recordHostAdmission(crawlerPageRequest, "crawl.example", 25*time.Millisecond, true)
	activity.recordHTTPRequestCompleted(crawlerPageRequest, "crawl.example", 75*time.Millisecond, 200, false)
	worker := &Worker{activity: activity}
	worker.logCrawlerProgress(
		context.Background(),
		"run-id",
		frontier.QueueDepth{Fetched: 4, Indexed: 6},
		frontier.QueueDepth{Fetched: 24, Indexed: 26, Queued: 200, Deferred: 5},
		3,
		9,
		time.Minute,
	)

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode progress log: %v", err)
	}
	if record["msg"] != messageCrawlerProgress || record["pagesFetchedPerMinute"] != float64(40) ||
		record["pagesIndexedPerMinute"] != float64(20) || record["frontierQueued"] != float64(200) ||
		record["pendingIngest"] != float64(9) || record["pendingIngestDelta"] != float64(6) {
		t.Fatalf("progress log fields = %#v", record)
	}
	if strings.Contains(output.String(), "\"url\"") || !strings.Contains(output.String(), "crawl.example") {
		t.Fatalf("progress log should include bounded host activity without URL fields: %s", output.String())
	}
}

func TestCrawlerFetchErrorTypeDoesNotExposeURL(t *testing.T) {
	fetchError := &url.Error{
		Op:  "Get",
		URL: "https://example.test/private-path?query=synthetic",
		Err: errors.New("connection refused"),
	}
	if got := crawlerFetchErrorType(fetchError); got != "*url.Error" {
		t.Fatalf("fetch error type = %q", got)
	}
}

func TestSlowPageStageTimingUsesPageIdentityWithoutURLFields(t *testing.T) {
	previousLogger := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	logPageProcessingTiming(
		context.Background(), "page-id", "crawl.example", "http_fetch", 2*time.Second, false,
	)
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("slow stage log records = %d, want 1", len(lines))
	}
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode stage log: %v", err)
		}
		if record["pageId"] != "page-id" || record["host"] != "crawl.example" || record["stage"] != "http_fetch" {
			t.Fatalf("stage log identity = %#v", record)
		}
		if _, found := record["url"]; found {
			t.Fatalf("stage log exposed a URL field: %#v", record)
		}
	}
}

func TestFastPageStageTimingDoesNotEmitALog(t *testing.T) {
	previousLogger := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	logPageProcessingTiming(
		context.Background(), "page-id", "crawl.example", "html_parse", 100*time.Millisecond, false,
	)
	if output.Len() != 0 {
		t.Fatalf("fast page stage emitted a log: %s", output.String())
	}
}
