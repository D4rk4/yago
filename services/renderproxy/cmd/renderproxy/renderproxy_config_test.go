package main

import "testing"

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func baseEnv() map[string]string {
	return map[string]string{"RENDERPROXY_CDP_URL": "ws://browser:9222"}
}

func TestLoadServiceConfigDefaults(t *testing.T) {
	cfg, err := LoadServiceConfig(envFrom(baseEnv()))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ListenAddr != DefaultListenAddr {
		t.Fatalf("listen addr = %q", cfg.ListenAddr)
	}
	if cfg.RenderConcurrency != DefaultRenderConcurrency {
		t.Fatalf("render concurrency = %d", cfg.RenderConcurrency)
	}
	if cfg.RequestDeadline != DefaultRequestDeadline {
		t.Fatalf("request deadline = %v", cfg.RequestDeadline)
	}
	if cfg.MaxResponseBytes != DefaultMaxResponseBytes {
		t.Fatalf("max response bytes = %d", cfg.MaxResponseBytes)
	}
	if cfg.OpsAddr != DefaultOpsAddr {
		t.Fatalf("ops addr = %q", cfg.OpsAddr)
	}
}

func TestLoadServiceConfigRequiresCDPURL(t *testing.T) {
	if _, err := LoadServiceConfig(envFrom(nil)); err == nil {
		t.Fatal("expected error for missing CDP URL")
	}
}

func TestLoadServiceConfigRejectsNonPositiveValues(t *testing.T) {
	cases := map[string]string{
		"RENDERPROXY_RENDER_CONCURRENCY": "0",
		"RENDERPROXY_REQUEST_DEADLINE":   "0s",
		"RENDERPROXY_MAX_RESPONSE_BYTES": "-1",
	}
	for key, value := range cases {
		env := baseEnv()
		env[key] = value
		if _, err := LoadServiceConfig(envFrom(env)); err == nil {
			t.Fatalf("expected error for %s=%s", key, value)
		}
	}
}
