package yagonode

import (
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestLoadCrawlConfigDefaultsToLoopbackWhenUnset(t *testing.T) {
	cfg, err := loadCrawlConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadCrawlConfig: %v", err)
	}
	if !cfg.Enabled() {
		t.Fatal("crawl RPC should default on so the co-located crawler connects")
	}
	if cfg.ListenAddr != defaultCrawlRPCAddr {
		t.Fatalf("ListenAddr = %q, want default %q", cfg.ListenAddr, defaultCrawlRPCAddr)
	}
	if !cfg.QualityGate {
		t.Fatal("quality gate must default on")
	}
	if cfg.FetchWorkers != yagocrawlcontract.DefaultFetchWorkerConcurrency {
		t.Fatalf("fetch workers = %d, want %d", cfg.FetchWorkers,
			yagocrawlcontract.DefaultFetchWorkerConcurrency)
	}
	if cfg.ProcessPagesPerSecond != yagocrawlcontract.DefaultProcessPagesPerSecond {
		t.Fatalf("process pages per second = %d, want %d", cfg.ProcessPagesPerSecond,
			yagocrawlcontract.DefaultProcessPagesPerSecond)
	}
	if cfg.MaximumRedirects != yagocrawlcontract.DefaultMaximumPageRedirects {
		t.Fatalf("maximum redirects = %d, want %d", cfg.MaximumRedirects,
			yagocrawlcontract.DefaultMaximumPageRedirects)
	}
	if cfg.MaxActiveRuns != yagocrawlcontract.DefaultActiveCrawlRunConcurrency {
		t.Fatalf(
			"maximum active runs = %d, want %d",
			cfg.MaxActiveRuns,
			yagocrawlcontract.DefaultActiveCrawlRunConcurrency,
		)
	}
	if cfg.MaxPagesPerRun != yagocrawlcontract.DefaultMaxPagesPerRun {
		t.Fatalf("max pages per run = %d, want %d", cfg.MaxPagesPerRun,
			yagocrawlcontract.DefaultMaxPagesPerRun)
	}
	if !cfg.PrioritizeAutomaticDiscovery {
		t.Fatal("automatic discovery priority must default on")
	}
}

func TestLoadCrawlConfigRejectsInvalidRuntimePolicy(t *testing.T) {
	if _, err := loadCrawlConfig(func(key string) string {
		if key == envCrawlerRequestTimeout {
			return "soon"
		}

		return ""
	}); err == nil {
		t.Fatal("invalid crawler runtime policy accepted")
	}
}

func TestLoadRuntimeCrawlConfigUsesNodeDataDirectory(t *testing.T) {
	dataDirectory := t.TempDir()
	config, err := loadRuntimeCrawlConfig(
		func(string) string { return "" },
		dataDirectory,
	)
	if err != nil {
		t.Fatalf("load runtime crawl config: %v", err)
	}
	want := filepath.Join(dataDirectory, crawlBrokerStateFileName)
	if config.StatePath != want {
		t.Fatalf("StatePath = %q, want %q", config.StatePath, want)
	}
}

func TestLoadCrawlConfigDisabledByOffSentinel(t *testing.T) {
	for _, v := range []string{"off", " OFF ", "Off"} {
		env := map[string]string{envCrawlRPCAddr: v}
		cfg, err := loadCrawlConfig(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("loadCrawlConfig(%q): %v", v, err)
		}
		if cfg.Enabled() {
			t.Fatalf("crawl RPC should be disabled for %q", v)
		}
	}
}

func TestLoadCrawlConfigEnabledWithAddr(t *testing.T) {
	env := map[string]string{envCrawlRPCAddr: " :9091 ", envIngestQualityGate: "false"}
	cfg, err := loadCrawlConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("loadCrawlConfig: %v", err)
	}
	if !cfg.Enabled() {
		t.Fatal("crawl should be enabled")
	}
	if cfg.ListenAddr != ":9091" {
		t.Fatalf("ListenAddr = %q, want trimmed :9091", cfg.ListenAddr)
	}
	if cfg.QualityGate {
		t.Fatal("quality gate opt-out ignored")
	}
}

func TestLoadCrawlConfigRejectsBadQualityGateValue(t *testing.T) {
	env := map[string]string{envIngestQualityGate: "maybe"}
	if _, err := loadCrawlConfig(func(k string) string { return env[k] }); err == nil {
		t.Fatal("expected bad bool error")
	}
}

func TestLoadCrawlConfigReadsCrawlerRuntimeSettings(t *testing.T) {
	env := map[string]string{
		envCrawlerWorkers:               "20",
		envCrawlerProcessPagesPerSecond: "23",
		envCrawlerMaximumRedirects:      "7",
		envCrawlerMaxActiveRuns:         "37",
		envCrawlerMaxPagesPerRun:        "1234",
		envPrioritizeAutomaticDiscovery: "false",
	}
	config, err := loadCrawlConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("load crawl config: %v", err)
	}
	if config.FetchWorkers != 20 || config.ProcessPagesPerSecond != 23 ||
		config.MaximumRedirects != 7 ||
		config.MaxActiveRuns != 37 ||
		config.MaxPagesPerRun != 1234 ||
		config.PrioritizeAutomaticDiscovery {
		t.Fatalf("crawler runtime settings = %+v", config)
	}
}

func TestLoadCrawlConfigDoesNotReadJoinedLegacyCrawlerEnvironmentName(t *testing.T) {
	env := map[string]string{
		"YAGO" + "CRAWLER_WORKERS":         "20",
		"YAGO" + "CRAWLER_MAX_ACTIVE_RUNS": "64",
	}
	config, err := loadCrawlConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("load crawl config: %v", err)
	}
	if config.FetchWorkers != yagocrawlcontract.DefaultFetchWorkerConcurrency {
		t.Fatalf("fetch workers = %d, want default %d",
			config.FetchWorkers, yagocrawlcontract.DefaultFetchWorkerConcurrency)
	}
	if config.MaxActiveRuns != yagocrawlcontract.DefaultActiveCrawlRunConcurrency {
		t.Fatalf(
			"maximum active runs = %d, want default %d",
			config.MaxActiveRuns,
			yagocrawlcontract.DefaultActiveCrawlRunConcurrency,
		)
	}
}

func TestLoadCrawlConfigRejectsInvalidCrawlerRuntimeSettings(t *testing.T) {
	for key, value := range map[string]string{
		envCrawlerWorkers:               "257",
		envCrawlerProcessPagesPerSecond: "1000001",
		envCrawlerMaximumRedirects:      "1001",
		envCrawlerMaxActiveRuns:         "257",
		envCrawlerMaxPagesPerRun:        "-1",
		envPrioritizeAutomaticDiscovery: "sometimes",
	} {
		env := map[string]string{key: value}
		if _, err := loadCrawlConfig(func(name string) string { return env[name] }); err == nil {
			t.Fatalf("expected %s=%q to fail", key, value)
		}
	}
}
