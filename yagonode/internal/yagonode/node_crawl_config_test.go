package yagonode

import "testing"

func TestLoadCrawlConfigDisabledWhenNoAddr(t *testing.T) {
	cfg, err := loadCrawlConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadCrawlConfig: %v", err)
	}
	if cfg.Enabled() {
		t.Fatal("crawl should be disabled without a crawl RPC address")
	}
	if !cfg.QualityGate {
		t.Fatal("quality gate must default on")
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
