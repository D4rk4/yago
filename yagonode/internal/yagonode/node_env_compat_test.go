package yagonode

import "testing"

func TestWithLegacyEnvAliasesPrefersCurrentName(t *testing.T) {
	env := map[string]string{"YAGO_PEER_NAME": "new", "YACY_PEER_NAME": "old"}
	getenv := withLegacyEnvAliases(func(name string) string { return env[name] })

	if got := getenv("YAGO_PEER_NAME"); got != "new" {
		t.Fatalf("YAGO_ value = %q, want new", got)
	}
}

func TestWithLegacyEnvAliasesFallsBackToLegacyName(t *testing.T) {
	env := map[string]string{"YACY_DATA_DIR": "/legacy"}
	getenv := withLegacyEnvAliases(func(name string) string { return env[name] })

	if got := getenv("YAGO_DATA_DIR"); got != "/legacy" {
		t.Fatalf("fallback value = %q, want /legacy", got)
	}
}

func TestWithLegacyEnvAliasesEmptyWhenNeitherSet(t *testing.T) {
	getenv := withLegacyEnvAliases(func(string) string { return "" })

	if got := getenv("YAGO_ANNOUNCE_INTERVAL"); got != "" {
		t.Fatalf("value = %q, want empty", got)
	}
}

func TestWithLegacyEnvAliasesPassesThroughNonPrefixedNames(t *testing.T) {
	env := map[string]string{"HOME": "/root"}
	getenv := withLegacyEnvAliases(func(name string) string { return env[name] })

	if got := getenv("HOME"); got != "/root" {
		t.Fatalf("passthrough value = %q, want /root", got)
	}
}

func TestWithLegacyEnvAliasesRejectsInventedCrawlerName(t *testing.T) {
	env := map[string]string{"YACY_CRAWLER_WORKERS": "99"}
	getenv := withLegacyEnvAliases(func(name string) string { return env[name] })

	if got := getenv("YAGO_CRAWLER_WORKERS"); got != "" {
		t.Fatalf("crawler fallback value = %q, want empty", got)
	}
	env["YAGO_CRAWLER_WORKERS"] = "4"
	if got := getenv("YAGO_CRAWLER_WORKERS"); got != "4" {
		t.Fatalf("canonical crawler value = %q, want 4", got)
	}
}

func TestWithLegacyEnvAliasesRejectsFuturePrefixMatches(t *testing.T) {
	env := map[string]string{"YACY_WEB_FALLBACK_PRIVACY": "always"}
	getenv := withLegacyEnvAliases(func(name string) string { return env[name] })

	if got := getenv("YAGO_WEB_FALLBACK_PRIVACY"); got != "" {
		t.Fatalf("future fallback value = %q, want empty", got)
	}
}

func TestLegacyEnvironmentAliasesAreExactHistoricalPairs(t *testing.T) {
	if len(legacyNodeEnvironmentAliases) != 28 {
		t.Fatalf("legacy alias count = %d, want 28", len(legacyNodeEnvironmentAliases))
	}
	for current, legacy := range legacyNodeEnvironmentAliases {
		env := map[string]string{legacy: current}
		getenv := withLegacyEnvAliases(func(name string) string { return env[name] })
		if got := getenv(current); got != current {
			t.Fatalf("%s fallback = %q, want %q", current, got, current)
		}
	}
}
