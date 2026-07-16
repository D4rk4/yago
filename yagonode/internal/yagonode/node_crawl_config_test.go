package yagonode

import (
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
	if !cfg.PrioritizeAutomaticDiscovery {
		t.Fatal("automatic discovery priority must default on")
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
		envPrioritizeAutomaticDiscovery: "false",
	}
	config, err := loadCrawlConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("load crawl config: %v", err)
	}
	if config.FetchWorkers != 20 || config.PrioritizeAutomaticDiscovery {
		t.Fatalf("crawler runtime settings = %+v", config)
	}
}

func TestLoadCrawlConfigRejectsInvalidCrawlerRuntimeSettings(t *testing.T) {
	for key, value := range map[string]string{
		envCrawlerWorkers:               "257",
		envPrioritizeAutomaticDiscovery: "sometimes",
	} {
		env := map[string]string{key: value}
		if _, err := loadCrawlConfig(func(name string) string { return env[name] }); err == nil {
			t.Fatalf("expected %s=%q to fail", key, value)
		}
	}
}
