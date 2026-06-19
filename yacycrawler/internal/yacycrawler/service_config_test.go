package yacycrawler_test

import (
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/yacycrawler"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadServiceConfigRequiresNATSURL(t *testing.T) {
	if _, err := yacycrawler.LoadServiceConfig(envFrom(nil)); err == nil {
		t.Fatal("expected error when NATS_URL is unset")
	}
}

func TestLoadServiceConfigDefaults(t *testing.T) {
	cfg, err := yacycrawler.LoadServiceConfig(envFrom(map[string]string{
		yacycrawler.EnvNATSURL: "nats://localhost:4222",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.OrdersSubject != yacycrawler.DefaultOrdersSubject {
		t.Errorf("orders subject = %q", cfg.OrdersSubject)
	}
	if cfg.IngestSubject != yacycrawler.DefaultIngestSubject {
		t.Errorf("ingest subject = %q", cfg.IngestSubject)
	}
	if cfg.OrdersDurable != yacycrawler.DefaultOrdersDurable {
		t.Errorf("durable = %q", cfg.OrdersDurable)
	}
	if cfg.IngestMaxMsgs != yacycrawler.DefaultIngestMaxMsgs {
		t.Errorf("max msgs = %d", cfg.IngestMaxMsgs)
	}
	spec := cfg.StreamSpec()
	if spec.OrdersSubject != cfg.OrdersSubject ||
		spec.IngestSubject != cfg.IngestSubject ||
		spec.IngestMaxMsgs != cfg.IngestMaxMsgs {
		t.Errorf("stream spec mismatch: %+v", spec)
	}
}

func TestLoadServiceConfigOverrides(t *testing.T) {
	cfg, err := yacycrawler.LoadServiceConfig(envFrom(map[string]string{
		yacycrawler.EnvNATSURL:           "nats://localhost:4222",
		yacycrawler.EnvNATSOrdersSubject: "o.subject",
		yacycrawler.EnvNATSIngestSubject: "i.subject",
		yacycrawler.EnvNATSDurable:       "dur",
		yacycrawler.EnvNATSIngestMaxMsgs: "7",
		yacycrawler.EnvWorkers:           "3",
		yacycrawler.EnvMaxDepth:          "5",
		yacycrawler.EnvCrawlDelay:        "250ms",
		yacycrawler.EnvUserAgent:         "test-agent",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.OrdersSubject != "o.subject" || cfg.IngestSubject != "i.subject" {
		t.Errorf("subjects = %q %q", cfg.OrdersSubject, cfg.IngestSubject)
	}
	if cfg.OrdersDurable != "dur" || cfg.IngestMaxMsgs != 7 {
		t.Errorf("durable/maxmsgs = %q %d", cfg.OrdersDurable, cfg.IngestMaxMsgs)
	}
	if cfg.Crawl.Workers != 3 || cfg.Crawl.MaxDepth != 5 {
		t.Errorf("workers/depth = %d %d", cfg.Crawl.Workers, cfg.Crawl.MaxDepth)
	}
	if cfg.Crawl.CrawlDelay != 250*time.Millisecond {
		t.Errorf("delay = %v", cfg.Crawl.CrawlDelay)
	}
	if cfg.Crawl.UserAgent != "test-agent" {
		t.Errorf("user agent = %q", cfg.Crawl.UserAgent)
	}
}

func TestLoadServiceConfigRejectsInvalidValues(t *testing.T) {
	base := map[string]string{yacycrawler.EnvNATSURL: "nats://localhost:4222"}
	cases := map[string]string{
		yacycrawler.EnvWorkers:           "0",
		yacycrawler.EnvMaxDepth:          "abc",
		yacycrawler.EnvCrawlDelay:        "-1s",
		yacycrawler.EnvNATSIngestMaxMsgs: "0",
	}
	for key, bad := range cases {
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		env[key] = bad
		if _, err := yacycrawler.LoadServiceConfig(envFrom(env)); err == nil {
			t.Errorf("%s=%q: expected error", key, bad)
		}
	}
}
