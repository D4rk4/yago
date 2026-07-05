package main

import (
	"strings"
	"testing"
)

func TestDefaultUserAgentCarriesBuildVersion(t *testing.T) {
	if version == "" {
		t.Fatal("build version must not be empty")
	}
	if version == "0.1" {
		t.Fatal("build version must be the calendar brand version, not the old stub")
	}
	want := "yago-crawler/" + version + " (+https://github.com/D4rk4/yago/)"
	if DefaultUserAgent != want {
		t.Fatalf("default user agent = %q, want %q", DefaultUserAgent, want)
	}
	if !strings.Contains(DefaultUserAgent, version) {
		t.Fatalf("user agent %q does not carry the version", DefaultUserAgent)
	}
}

func TestDefaultUserAgentReachesCrawlConfig(t *testing.T) {
	cfg, err := LoadServiceConfig(func(key string) string {
		if key == EnvNodeRPCAddr {
			return "node:9091"
		}

		return ""
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Crawl.UserAgent != DefaultUserAgent {
		t.Fatalf("crawl user agent = %q, want the branded default", cfg.Crawl.UserAgent)
	}
}
