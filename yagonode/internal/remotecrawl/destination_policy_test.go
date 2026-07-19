package remotecrawl

import (
	"context"
	"net/netip"
	"testing"
)

func TestDestinationPolicyRequiresExactDomainAndSafeResolution(t *testing.T) {
	policy, err := newDestinationPolicy([]string{"example.com"}, publicResolver)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := policy.Admit(t.Context(), "HTTPS://EXAMPLE.COM:443/a#fragment")
	if err != nil || canonical != "https://example.com/a" {
		t.Fatalf("Admit = %q, %v", canonical, err)
	}
	for _, rawURL := range []string{
		"file:///etc/passwd",
		"https://user@example.com/a",
		"https://sub.example.com/a",
		"https://example.com:8443/a",
	} {
		if _, err := policy.Admit(t.Context(), rawURL); err == nil {
			t.Errorf("Admit(%q) succeeded", rawURL)
		}
	}
}

func TestDestinationPolicyRejectsReboundPrivateAddress(t *testing.T) {
	resolver := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	policy, err := newDestinationPolicy([]string{"example.com"}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Admit(t.Context(), testURLA); err == nil {
		t.Fatal("private resolution admitted")
	}
}

func TestDestinationPolicyAllowsExplicitPrivateCIDRButNeverLoopback(t *testing.T) {
	policy, err := newDestinationPolicy([]string{"10.20.0.0/16"}, publicResolver)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := policy.Admit(t.Context(), "http://10.20.1.2/a")
	if err != nil || canonical != "http://10.20.1.2/a" {
		t.Fatalf("private allowlist = %q, %v", canonical, err)
	}
	loopback, err := newDestinationPolicy([]string{"127.0.0.0/8"}, publicResolver)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loopback.Admit(t.Context(), "http://127.0.0.1/a"); err == nil {
		t.Fatal("loopback allowlist admitted")
	}
}

func TestDestinationPolicyRejectsAddressFamilyWildcards(t *testing.T) {
	for _, destination := range []string{"0.0.0.0/0", "::/0"} {
		if _, err := NormalizeAllowedDestinations([]string{destination}); err == nil {
			t.Fatalf("destination %q accepted", destination)
		}
	}
}
