package yagonode

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

func TestApplyCrawlAdminOptionsClearsPendingOrders(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	options := &adminui.Options{}
	applyCrawlAdminOptions(
		options,
		node{crawl: runtime},
		crawlQueueDepthSource{probe: runtime.crawlQueueDepth},
	)
	if options.CrawlOrderClearSource == nil {
		t.Fatal("crawl order clear source missing for a live runtime")
	}
	order := yagocrawlcontract.CrawlOrder{
		Profile: yagocrawlcontract.NewCrawlProfile(
			yagocrawlcontract.CrawlProfile{Name: "clear-source"},
		),
		Requests: []yagocrawlcontract.CrawlRequest{{URL: "https://queue.example/"}},
	}
	if err := runtime.broker.Orders.Publish(t.Context(), order); err != nil {
		t.Fatalf("publish order: %v", err)
	}
	removed, err := options.CrawlOrderClearSource.ClearPendingOrders(t.Context())
	if err != nil || removed != 1 {
		t.Fatalf("clear pending orders = %d, %v, want 1, nil", removed, err)
	}
	depth, err := runtime.broker.Orders.Depth(t.Context())
	if err != nil || depth.Pending != 0 || depth.Leased != 0 {
		t.Fatalf("queue depth = %+v, %v, want empty", depth, err)
	}
}

func TestApplyCrawlAdminOptionsHidesOrderClearWithoutRuntime(t *testing.T) {
	options := &adminui.Options{}
	applyCrawlAdminOptions(options, node{}, crawlQueueDepthSource{})
	if options.CrawlOrderClearSource != nil {
		t.Fatal("crawl order clear source available without a crawl runtime")
	}
}

func TestNewCrawlOrderClearSourceRefusesUnavailableRuntime(t *testing.T) {
	var typedNil *crawlRuntime
	tests := []struct {
		name    string
		runtime crawlProcess
	}{
		{name: "nil runtime"},
		{name: "typed nil runtime", runtime: typedNil},
		{name: "missing broker", runtime: &crawlRuntime{}},
		{
			name:    "missing order queue",
			runtime: &crawlRuntime{broker: &crawlbroker.CrawlBroker{}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if source := newCrawlOrderClearSource(test.runtime); source != nil {
				t.Fatal("crawl order clear source available without an order queue")
			}
		})
	}
}

func TestCrawlOrderClearFailureLogsOnlyCountAndServerSpan(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	source := newCrawlOrderClearSource(runtime)
	previous := slog.Default()
	var output concurrentLogCapture
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	serverContext, span := tracectx.StartServerSpan(t.Context(), "")
	canceledContext, cancel := context.WithCancel(serverContext)
	cancel()

	removed, err := source.ClearPendingOrders(canceledContext)
	if err == nil || removed != 0 {
		t.Fatalf("clear canceled queue = %d, %v, want 0 and error", removed, err)
	}
	var fields map[string]any
	if err := json.Unmarshal(output.Bytes(), &fields); err != nil {
		t.Fatalf("decode clear warning: %v", err)
	}
	if fields["serverSpanId"] != span.SpanID || fields["removed"] != float64(0) {
		t.Fatalf("clear warning fields = %v", fields)
	}
	if _, exists := fields["error"]; exists {
		t.Fatal("clear warning exposed the error")
	}
	if _, exists := fields["url"]; exists {
		t.Fatal("clear warning exposed a URL")
	}
}
