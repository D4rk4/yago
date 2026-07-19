package yagonode

import (
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagoegress"
)

func TestEgressCIDRBootstrapAdminAndRuntimeShareCanonicalValues(t *testing.T) {
	raw := "fd00:1::9/48,10.1.2.99/24,10.1.2.1/24"
	_, _, bootstrap, err := egressConfig(environmentValues{envEgressAllowCIDRs: raw}.get)
	if err != nil {
		t.Fatal(err)
	}
	want := "10.1.2.0/24,fd00:1::/48"
	if formatPrefixes(bootstrap) != want {
		t.Fatalf("bootstrap prefixes = %q, want %q", formatPrefixes(bootstrap), want)
	}

	definition := indexSettingDefinitions()["security.egress.allow_cidrs"]
	normalized, err := definition.normalize(raw)
	if err != nil || normalized != want {
		t.Fatalf("Admin normalization = %q, %v", normalized, err)
	}
	applied := definition.apply(nodeConfig{}, normalized)
	if formatPrefixes(applied.EgressAllowedCIDRs) != want {
		t.Fatalf("Admin prefixes = %q, want %q", formatPrefixes(applied.EgressAllowedCIDRs), want)
	}

	guard := yagoegress.NewGuard(
		false,
		yagoegress.WithPrivateAllowlist(applied.EgressAllowedCIDRs),
	)
	for _, address := range []netip.Addr{
		netip.MustParseAddr("10.1.2.7"),
		netip.MustParseAddr("fd00:1::7"),
	} {
		if err := guard.AdmitAddr(address); err != nil {
			t.Fatalf("allowlisted address %s rejected: %v", address, err)
		}
	}
	if err := guard.AdmitAddr(
		netip.MustParseAddr("10.2.0.1"),
	); !errors.Is(
		err,
		yagoegress.ErrBlocked,
	) {
		t.Fatalf("unlisted address error = %v", err)
	}
}

func TestEgressCIDRBootstrapAndAdminRejectTheSameInvalidValues(t *testing.T) {
	definition := indexSettingDefinitions()["security.egress.allow_cidrs"]
	invalid := []string{
		"10.1.2.3",
		"10.0.0.0/99",
		strings.Repeat("x", maximumEgressAllowCIDRConfigurationBytes+1),
	}
	for _, raw := range invalid {
		if _, err := parseEgressAllowCIDRs(raw); err == nil {
			t.Fatalf("bootstrap accepted %q", raw)
		}
		if _, err := definition.normalize(raw); err == nil {
			t.Fatalf("Admin accepted %q", raw)
		}
	}

	many := make([]string, maximumEgressAllowCIDRs+1)
	for index := range many {
		many[index] = netip.PrefixFrom(netip.AddrFrom4([4]byte{10, 0, byte(index), 0}), 24).String()
	}
	raw := strings.Join(many, ",")
	if _, err := parseEgressAllowCIDRs(raw); err == nil {
		t.Fatal("bootstrap accepted too many CIDRs")
	}
	if _, err := definition.normalize(raw); err == nil {
		t.Fatal("Admin accepted too many CIDRs")
	}
}
