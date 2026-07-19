package yagonode

import (
	"strings"
	"testing"
)

func TestPeerNameValueBounds(t *testing.T) {
	if got, err := parsePeerName("  node-ä  "); err != nil || got != "node-ä" {
		t.Fatalf("peer name = %q, %v", got, err)
	}
	for _, value := range []string{strings.Repeat("a", maximumPeerNameBytes+1), "bad\nname", string([]byte{0xff})} {
		if _, err := parsePeerName(value); err == nil {
			t.Fatalf("peer name %q must fail", value)
		}
	}
}

func TestAdvertiseHostValueIsHostOnly(t *testing.T) {
	accepted := map[string]string{
		"Example.COM":      "example.com",
		"[2001:db8::1]":    "2001:db8::1",
		"::ffff:192.0.2.1": "192.0.2.1",
		"":                 "",
	}
	for value, expected := range accepted {
		got, err := parseAdvertiseHost(value)
		if err != nil || got != expected {
			t.Fatalf("advertised host %q = %q, %v", value, got, err)
		}
	}
	for _, value := range []string{"https://example.com", "example.com:8090", "user@example.com", "fe80::1%eth0"} {
		if _, err := parseAdvertiseHost(value); err == nil {
			t.Fatalf("advertised host %q must fail", value)
		}
	}
}

func TestSeedlistURLValuesAreBoundedCanonicalHTTPURLs(t *testing.T) {
	got, err := parseSeedlistURLs(
		" HTTPS://Example.COM:443/seeds.txt?network=freeworld ,https://example.com/seeds.txt?network=freeworld ",
	)
	if err != nil {
		t.Fatalf("parse seedlist URLs: %v", err)
	}
	if len(got) != 1 || got[0] != "https://example.com/seeds.txt?network=freeworld" {
		t.Fatalf("seedlist URLs = %#v", got)
	}
	for _, value := range []string{
		"file:///tmp/seeds",
		"https://user:secret@example.com/seeds",
		"https://example.com/seeds#fragment",
		strings.Repeat("x", maximumSeedlistConfigurationBytes+1),
	} {
		if _, err := parseSeedlistURLs(value); err == nil {
			t.Fatalf("seedlist URLs %q must fail", value)
		}
	}
	tooMany := make([]string, maximumSeedlistURLs+1)
	for index := range tooMany {
		tooMany[index] = "https://example.com/" + strings.Repeat("a", index+1)
	}
	if _, err := parseSeedlistURLs(strings.Join(tooMany, ",")); err == nil {
		t.Fatal("too many seedlist URLs must fail")
	}
}

func TestNetworkBootstrapRejectsUnsafeOperatorValues(t *testing.T) {
	base := map[string]string{envPeerHash: "0123456789AB", envPeerName: "node"}
	for key, value := range map[string]string{
		envPeerName:       strings.Repeat("a", maximumPeerNameBytes+1),
		envAdvertiseHost:  "https://example.com",
		envSeedlistURLs:   "file:///tmp/seeds",
		envGreetsPerCycle: "1025",
	} {
		environment := map[string]string{key: value}
		for name, configured := range base {
			if _, exists := environment[name]; !exists {
				environment[name] = configured
			}
		}
		if _, err := loadNodeConfig(envFrom(environment)); err == nil {
			t.Fatalf("%s=%q must fail", key, value)
		}
	}
}
