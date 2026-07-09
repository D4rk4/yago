package main

import (
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func baseEnv() map[string]string {
	return map[string]string{
		"NATS_URL": "nats://localhost:4222",
	}
}

func TestLoadServiceConfigDefaults(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(baseEnv()))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.OrdersSubject != DefaultOrdersSubject {
		t.Fatalf("orders subject = %q", cfg.OrdersSubject)
	}
	if cfg.ListenAddr != DefaultListenAddr || cfg.OpsAddr != DefaultOpsAddr {
		t.Fatalf("unexpected addr defaults: %+v", cfg)
	}
	if cfg.OrderTimeout != DefaultOrderTimeout {
		t.Fatalf("order timeout = %v", cfg.OrderTimeout)
	}
	if cfg.MaxInFlight != DefaultMaxInFlight {
		t.Fatalf("max in flight = %d", cfg.MaxInFlight)
	}
	if cfg.MaxBodyBytes != DefaultMaxBodyBytes {
		t.Fatalf("max body bytes = %d", cfg.MaxBodyBytes)
	}
	if cfg.CrawlProfile.Scope != yacycrawlcontract.ScopeDomain {
		t.Fatalf("scope = %v, want domain", cfg.CrawlProfile.Scope)
	}
	if cfg.CrawlProfile.URLMustMatch != yacycrawlcontract.MatchAll {
		t.Fatalf("urlMustMatch = %q, want MatchAll", cfg.CrawlProfile.URLMustMatch)
	}
	if cfg.CrawlProfile.MaxDepth != DefaultCrawlMaxDepth {
		t.Fatalf("max depth = %d", cfg.CrawlProfile.MaxDepth)
	}
	if cfg.CrawlProfile.MaxPagesPerHost != DefaultCrawlMaxPagesPerHost {
		t.Fatalf("max pages per host = %d", cfg.CrawlProfile.MaxPagesPerHost)
	}
	if cfg.CrawlProfile.Handle == "" {
		t.Fatal("profile handle is empty")
	}
}

func TestLoadServiceConfigRequiresNATSURL(t *testing.T) {
	env := baseEnv()
	delete(env, "NATS_URL")
	if _, err := LoadServiceConfig(envFrom(env)); err == nil {
		t.Fatal("missing NATS_URL should error")
	}
}

func TestLoadServiceConfigOverrides(t *testing.T) {
	env := baseEnv()
	env["YACYVISITCRAWL_LISTEN_ADDR"] = ":9000"
	env["YACYVISITCRAWL_ORDER_TIMEOUT"] = "2s"
	env["YACYVISITCRAWL_MAX_IN_FLIGHT"] = "8"
	env["YACYVISITCRAWL_CRAWL_SCOPE"] = "wide"
	env["YACYVISITCRAWL_CRAWL_MAX_DEPTH"] = "3"
	env["YACYVISITCRAWL_CRAWL_ALLOW_QUERY_URLS"] = "true"
	env["YACYVISITCRAWL_CRAWL_DELAY"] = "500ms"
	cfg, err := LoadServiceConfig(envFrom(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":9000" || cfg.OrderTimeout != 2*time.Second || cfg.MaxInFlight != 8 {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
	if cfg.CrawlProfile.Scope != yacycrawlcontract.ScopeWide || cfg.CrawlProfile.MaxDepth != 3 {
		t.Fatalf("crawl profile overrides not applied: %+v", cfg.CrawlProfile)
	}
	if !cfg.CrawlProfile.AllowQueryURLs {
		t.Fatal("allow query urls should be true")
	}
	if cfg.CrawlProfile.CrawlDelay != 500*time.Millisecond {
		t.Fatalf("crawl delay = %v", cfg.CrawlProfile.CrawlDelay)
	}
}

func TestLoadServiceConfigRejectsUnknownScope(t *testing.T) {
	env := baseEnv()
	env["YACYVISITCRAWL_CRAWL_SCOPE"] = "galaxy"
	if _, err := LoadServiceConfig(envFrom(env)); err == nil {
		t.Fatal("unknown scope should error")
	}
}

func TestLoadServiceConfigRejectsBadValues(t *testing.T) {
	for _, bad := range []map[string]string{
		{"YACYVISITCRAWL_ORDER_TIMEOUT": "0s"},
		{"YACYVISITCRAWL_ORDER_TIMEOUT": "nope"},
		{"YACYVISITCRAWL_MAX_IN_FLIGHT": "0"},
		{"YACYVISITCRAWL_MAX_BODY_BYTES": "-1"},
		{"YACYVISITCRAWL_CRAWL_MAX_DEPTH": "-1"},
		{"YACYVISITCRAWL_CRAWL_MAX_PAGES_PER_HOST": "0"},
		{"YACYVISITCRAWL_CRAWL_DELAY": "-1s"},
		{"YACYVISITCRAWL_CRAWL_ALLOW_QUERY_URLS": "maybe"},
	} {
		env := baseEnv()
		for k, v := range bad {
			env[k] = v
		}
		if _, err := LoadServiceConfig(envFrom(env)); err == nil {
			t.Errorf("expected error for %v", bad)
		}
	}
}

func TestOrdersStreamSpec(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(baseEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OrdersStreamSpec().Subject != DefaultOrdersSubject {
		t.Fatal("orders spec subject wrong")
	}
}
