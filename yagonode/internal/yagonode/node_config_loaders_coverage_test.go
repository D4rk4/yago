package yagonode

import "testing"

func envWithBad(key, bad string) func(string) string {
	return func(k string) string {
		if k == key {
			return bad
		}

		return ""
	}
}

func TestLoadDerivedConfigsRejectsBadEnv(t *testing.T) {
	badBool := map[string]string{
		envSearchRequireAPIKey: "notabool",
		envPublicSearchUI:      "notabool",
		envHTTPSRedirect:       "notabool",
		envMetricsEnabled:      "notabool",
	}
	for key, bad := range badBool {
		if _, err := loadDerivedConfigs(envWithBad(key, bad)); err == nil {
			t.Errorf("%s=%q: expected error", key, bad)
		}
	}
	if _, err := loadDerivedConfigs(envWithBad(envQueryLogMode, "bogus-mode")); err == nil {
		t.Error("bad query log mode: expected error")
	}
	if _, err := loadDerivedConfigs(
		envWithBad(envWebFallbackMaxResults, "notanumber"),
	); err == nil {
		t.Error("bad web fallback config: expected error")
	}
	if _, err := loadDerivedConfigs(
		envWithBad(envExtractFetchTimeout, "notaduration"),
	); err == nil {
		t.Error("bad extract fetch config: expected error")
	}
}

func TestLoadExtractFetchConfigRejectsBadEnv(t *testing.T) {
	cases := map[string]string{
		envExtractFetchEnabled:  "notabool",
		envExtractFetchTimeout:  "notaduration",
		envExtractFetchMaxBytes: "notanumber",
	}
	for key, bad := range cases {
		if _, err := loadExtractFetchConfig(envWithBad(key, bad)); err == nil {
			t.Errorf("%s=%q: expected error", key, bad)
		}
	}
}

func TestLoadWebFallbackConfigRejectsBadEnv(t *testing.T) {
	cases := map[string]string{
		envWebFallbackMaxResults:  "notanumber",
		envWebFallbackTimeout:     "notaduration",
		envWebFallbackCacheTTL:    "notaduration",
		envWebFallbackSeedCrawl:   "notabool",
		envWebFallbackSeedDepth:   "notanumber",
		envWebFallbackSeedMaxPage: "notanumber",
	}
	for key, bad := range cases {
		if _, err := loadWebFallbackConfig(envWithBad(key, bad)); err == nil {
			t.Errorf("%s=%q: expected error", key, bad)
		}
	}
}
