package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
)

func crawlEnabledNode(t *testing.T) (nodeConfig, node) {
	t.Helper()
	config := testConfig(t)
	config.Crawl = crawlConfig{ListenAddr: "127.0.0.1:0"}
	assembled := assembleTestNode(t, config, openTestVault(t))

	return config, assembled
}

func TestServeDoesNotStartNodeRecrawlSweep(t *testing.T) {
	previous := newRecrawlSweepTicks
	newRecrawlSweepTicks = func(time.Duration) (<-chan time.Time, func()) {
		t.Error("node recrawl sweep was started")
		return make(chan time.Time), func() {}
	}
	t.Cleanup(func() { newRecrawlSweepTicks = previous })
	_, assembled := crawlEnabledNode(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := serve(
		ctx,
		assembled,
		metrics.NewEvictionMetrics(prometheus.NewRegistry()),
		namedServer{
			"ops",
			buildServer(
				"127.0.0.1:0",
				newOpsMux(metrics.NewHTTPEndpointMetrics().Handler(), nil, nil, nil, nil),
			),
		},
	)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestBuildOpsMuxWiresRankingAndCrawlSurfaces(t *testing.T) {
	config, assembled := crawlEnabledNode(t)
	if assembled.crawl != nil {
		t.Cleanup(assembled.crawl.Close)
	}

	mux := buildOpsMux(
		metrics.NewHTTPEndpointMetrics(),
		config,
		assembled,
		events.NewRecorder(4),
		consoleAdminSources{},
	)
	if mux == nil {
		t.Fatal("buildOpsMux returned nil")
	}
}
