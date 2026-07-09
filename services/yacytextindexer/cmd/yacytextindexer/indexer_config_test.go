package main

import "testing"

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestStartReturnsNonZeroOnInvalidConfig(t *testing.T) {
	origLookup := lookupEnv
	lookupEnv = envFrom(nil)
	defer func() { lookupEnv = origLookup }()

	if code := start(); code != 2 {
		t.Errorf("start() = %d, want 2", code)
	}
}

func TestLoadServiceConfigRequiresNATSURL(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(nil)); err == nil {
		t.Fatal("expected error when NATS_URL is unset")
	}
}

func TestLoadServiceConfigRequiresSearchIndexEngine(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL: "nats://localhost:4222",
	})); err == nil {
		t.Fatal("expected error when SEARCH_INDEX_ENGINE is unset")
	}
}

func TestLoadServiceConfigRequiresElasticsearchURL(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:           "nats://localhost:4222",
		EnvSearchIndexEngine: SearchIndexEngineElasticsearch,
	})); err == nil {
		t.Fatal("expected error when ELASTICSEARCH_URL is unset")
	}
}

func TestLoadServiceConfigDefaults(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:           "nats://localhost:4222",
		EnvSearchIndexEngine: SearchIndexEngineElasticsearch,
		EnvElasticsearchURL:  "http://localhost:9200",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.CrawledPageSubject != DefaultCrawledPageSubject {
		t.Errorf("subject = %q", cfg.CrawledPageSubject)
	}
	if cfg.CrawledPageMaxMsgs != DefaultCrawledPageMaxMsgs {
		t.Errorf("max msgs = %d", cfg.CrawledPageMaxMsgs)
	}
	if cfg.CrawledPageDurable != DefaultCrawledPageDurable {
		t.Errorf("durable = %q", cfg.CrawledPageDurable)
	}
	if cfg.Concurrency != DefaultConcurrency {
		t.Errorf("concurrency = %d", cfg.Concurrency)
	}
	if cfg.ElasticsearchIndex != DefaultElasticsearchIndex {
		t.Errorf("index = %q", cfg.ElasticsearchIndex)
	}
	spec := cfg.CrawledPageStreamSpec()
	if spec.Subject != cfg.CrawledPageSubject || spec.MaxMsgs != cfg.CrawledPageMaxMsgs {
		t.Errorf("stream spec mismatch: %+v", spec)
	}
}

func TestLoadServiceConfigOverrides(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(map[string]string{
		EnvNATSURL:                "nats://localhost:4222",
		EnvSearchIndexEngine:      SearchIndexEngineElasticsearch,
		EnvElasticsearchURL:       "http://localhost:9200",
		EnvNATSCrawledPageSubject: "t.subject",
		EnvNATSCrawledPageMaxMsgs: "7",
		EnvNATSCrawledPageDurable: "dur",
		EnvConcurrency:            "3",
		EnvElasticsearchIndex:     "my-index",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.CrawledPageSubject != "t.subject" || cfg.CrawledPageMaxMsgs != 7 {
		t.Errorf("subject/maxmsgs = %q %d", cfg.CrawledPageSubject, cfg.CrawledPageMaxMsgs)
	}
	if cfg.CrawledPageDurable != "dur" || cfg.Concurrency != 3 {
		t.Errorf("durable/concurrency = %q %d", cfg.CrawledPageDurable, cfg.Concurrency)
	}
	if cfg.ElasticsearchIndex != "my-index" {
		t.Errorf("index = %q", cfg.ElasticsearchIndex)
	}
}

func TestLoadServiceConfigRejectsInvalidValues(t *testing.T) {
	base := map[string]string{
		EnvNATSURL:           "nats://localhost:4222",
		EnvSearchIndexEngine: SearchIndexEngineElasticsearch,
		EnvElasticsearchURL:  "http://localhost:9200",
	}
	cases := map[string]string{
		EnvNATSCrawledPageMaxMsgs: "0",
		EnvConcurrency:            "abc",
	}
	for key, bad := range cases {
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		env[key] = bad
		if _, err := LoadServiceConfig(envFrom(env)); err == nil {
			t.Errorf("%s=%q: expected error", key, bad)
		}
	}
}
