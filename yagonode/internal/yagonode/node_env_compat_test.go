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
