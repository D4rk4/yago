package yagonode

import "testing"

func TestLoadCrawlConfigDisabledWhenNoAddr(t *testing.T) {
	cfg := loadCrawlConfig(func(string) string { return "" })
	if cfg.Enabled() {
		t.Fatal("crawl should be disabled without a crawl RPC address")
	}
}

func TestLoadCrawlConfigEnabledWithAddr(t *testing.T) {
	env := map[string]string{envCrawlRPCAddr: " :9091 "}
	cfg := loadCrawlConfig(func(k string) string { return env[k] })
	if !cfg.Enabled() {
		t.Fatal("crawl should be enabled")
	}
	if cfg.ListenAddr != ":9091" {
		t.Fatalf("ListenAddr = %q, want trimmed :9091", cfg.ListenAddr)
	}
}
