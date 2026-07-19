package yagonode

import (
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagoproto"
)

func TestCrossOriginValueBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := normalizeCrossOriginList("https://example.com/path"); err == nil {
		t.Fatal("invalid origin normalized")
	}
	for input, expected := range map[string]string{
		"http://example.com:8080":     "http://example.com:8080",
		"https://[2001:db8::1]:8443/": "https://[2001:db8::1]:8443",
	} {
		got, err := canonicalCrossOrigin(input)
		if err != nil || got != expected {
			t.Fatalf("canonical origin %q = %q, %v", input, got, err)
		}
	}
	for _, host := range []string{"", "\u200d", "bad host"} {
		if _, _, err := canonicalCrossOriginHost(host); err == nil {
			t.Fatalf("origin host %q accepted", host)
		}
	}
	if _, err := canonicalCrossOrigin("https://\u200d"); err == nil {
		t.Fatal("origin with invalid host accepted")
	}
}

func TestNetworkBootstrapValueBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := parseAdvertiseHost(strings.Repeat("a", 254)); err == nil {
		t.Fatal("oversized advertised host accepted")
	}
	for input, expected := range map[string]string{
		"https://[2001:db8::1]:8443/seeds": "https://[2001:db8::1]:8443/seeds",
		"http://example.com:8080/seeds":    "http://example.com:8080/seeds",
	} {
		got, err := canonicalSeedlistURL(input)
		if err != nil || got != expected {
			t.Fatalf("canonical seedlist %q = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{
		"https://%",
		"https://example.com:0/seeds",
		"https://\u200d/seeds",
	} {
		if _, err := canonicalSeedlistURL(input); err == nil {
			t.Fatalf("seedlist URL %q accepted", input)
		}
	}
	if _, err := advertisePort(
		func(string) string { return "" },
		"localhost:not-a-port",
	); err == nil {
		t.Fatal("non-numeric peer listener port accepted for advertisement")
	}
}

func TestAuthenticationAndDurationRejectUnsupportedValues(t *testing.T) {
	t.Parallel()

	if err := validateNetworkAuthenticationSecret(
		yagoproto.NetworkAuthenticationMode("unknown"),
		"secret",
	); err == nil {
		t.Fatal("unsupported authentication mode accepted")
	}
	for _, value := range []string{"invalid", "0s", "-1s"} {
		if _, err := durationEnv(
			func(string) string { return value },
			"TEST_DURATION",
			time.Second,
		); err == nil {
			t.Fatalf("duration %q accepted", value)
		}
	}
}
