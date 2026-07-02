package main

import (
	"strings"
	"testing"
	"time"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadNodeConfigAppliesDefaults(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash: "0123456789AB",
		envPeerName: "node",
		envProxyURL: "http://proxy:4750",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if config.ProxyURL == nil || config.ProxyURL.String() != "http://proxy:4750" {
		t.Errorf("ProxyURL = %v", config.ProxyURL)
	}
	if config.PeerAddr != defaultPeerAddr {
		t.Errorf("PeerAddr = %q, want %q", config.PeerAddr, defaultPeerAddr)
	}
	if config.OpsAddr != defaultOpsAddr {
		t.Errorf("OpsAddr = %q, want %q", config.OpsAddr, defaultOpsAddr)
	}
	if config.AdvertisePort != 8090 {
		t.Errorf("AdvertisePort = %d, want 8090 (from peer addr)", config.AdvertisePort)
	}
	if config.PublicSelfTestURL == nil ||
		config.PublicSelfTestURL.String() != "http://127.0.0.1:8090" {
		t.Errorf("PublicSelfTestURL = %v", config.PublicSelfTestURL)
	}
	if !strings.HasSuffix(config.StoragePath, storageFileName) {
		t.Errorf("StoragePath = %q, want suffix %q", config.StoragePath, storageFileName)
	}
	if config.StorageQuotaByte != 1<<30 {
		t.Errorf("StorageQuotaByte = %d, want 1GB", config.StorageQuotaByte)
	}
	if !config.DHT.Gates.NetworkDHTEnabled ||
		!config.DHT.Gates.DistributionEnabled ||
		config.DHT.Gates.AllowWhileCrawling ||
		!config.DHT.Gates.AllowWhileIndexing ||
		config.DHT.Interval != defaultDHTDistributionInterval {
		t.Errorf("DHT config = %#v", config.DHT)
	}
}

func TestLoadNodeConfigReadsOverrides(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash:                "0123456789AB",
		envPeerName:                "node",
		envProxyURL:                "http://proxy:4750",
		envNetworkName:             "testnet",
		envPeerAddr:                ":7000",
		envOpsAddr:                 ":7001",
		envAdvertiseHost:           "203.0.113.1",
		envAdvertisePort:           "9999",
		envPublicSelfTestURL:       "https://public.example:9443",
		envStorageQuota:            "2MB",
		envTrustedProxies:          "10.0.0.0/8",
		envSeedlistURLs:            " http://a , http://b ,",
		envAnnounceInterval:        "30s",
		envNetworkDHT:              "false",
		envDHTDistribution:         "false",
		envDHTAllowWhileCrawling:   "true",
		envDHTAllowWhileIndexing:   "false",
		envDHTDistributionInterval: "45s",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if config.NetworkName != "testnet" {
		t.Errorf("NetworkName = %q", config.NetworkName)
	}
	if config.AdvertisePort != 9999 {
		t.Errorf("AdvertisePort = %d, want 9999", config.AdvertisePort)
	}
	if config.PublicSelfTestURL == nil ||
		config.PublicSelfTestURL.String() != "https://public.example:9443" {
		t.Errorf("PublicSelfTestURL = %v", config.PublicSelfTestURL)
	}
	if config.StorageQuotaByte != 2<<20 {
		t.Errorf("StorageQuotaByte = %d, want 2MB", config.StorageQuotaByte)
	}
	if len(config.TrustedProxies) != 1 {
		t.Errorf("TrustedProxies = %d, want 1", len(config.TrustedProxies))
	}
	if got := config.SeedlistURLs; len(got) != 2 || got[0] != "http://a" || got[1] != "http://b" {
		t.Errorf("SeedlistURLs = %v, want trimmed pair", got)
	}
	if config.AnnounceInterval != 30*time.Second {
		t.Errorf("AnnounceInterval = %v, want 30s", config.AnnounceInterval)
	}
	if config.DHT.Gates.NetworkDHTEnabled ||
		config.DHT.Gates.DistributionEnabled ||
		!config.DHT.Gates.AllowWhileCrawling ||
		config.DHT.Gates.AllowWhileIndexing ||
		config.DHT.Interval != 45*time.Second {
		t.Errorf("DHT config = %#v", config.DHT)
	}
}

func TestLoadNodeConfigDefaultsAnnounceInterval(t *testing.T) {
	config, err := loadNodeConfig(envFrom(map[string]string{
		envPeerHash: "0123456789AB",
		envPeerName: "node",
		envProxyURL: "http://proxy:4750",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.AnnounceInterval != defaultAnnounceInterval {
		t.Errorf("AnnounceInterval = %v, want default", config.AnnounceInterval)
	}
	if config.SeedlistURLs != nil {
		t.Errorf("SeedlistURLs = %v, want nil", config.SeedlistURLs)
	}
}

func TestLoadNodeConfigRejectsBadAnnounceInterval(t *testing.T) {
	base := map[string]string{
		envPeerHash: "0123456789AB",
		envPeerName: "node",
		envProxyURL: "http://proxy:4750",
	}
	for _, bad := range []string{"nope", "-1s"} {
		env := map[string]string{envAnnounceInterval: bad}
		for k, v := range base {
			env[k] = v
		}
		if _, err := loadNodeConfig(envFrom(env)); err == nil {
			t.Fatalf("%q: expected error", bad)
		}
	}
}

func TestLoadNodeConfigRejects(t *testing.T) {
	cases := map[string]map[string]string{
		"bad hash":     {envPeerHash: "short"},
		"missing name": {envPeerHash: "0123456789AB"},
		"announce no host": {
			envPeerHash:     "0123456789AB",
			envPeerName:     "n",
			envSeedlistURLs: "http://seed",
		},
		"bad port":         {envPeerHash: "0123456789AB", envPeerName: "n", envAdvertisePort: "-3"},
		"bad peer address": {envPeerHash: "0123456789AB", envPeerName: "n", envPeerAddr: "bad"},
		"bad public self-test url": {
			envPeerHash:          "0123456789AB",
			envPeerName:          "n",
			envPublicSelfTestURL: "file:///tmp/node",
		},
		"bad greet count": {
			envPeerHash:       "0123456789AB",
			envPeerName:       "n",
			envGreetsPerCycle: "many",
		},
		"bad quota": {envPeerHash: "0123456789AB", envPeerName: "n", envStorageQuota: "big"},
		"bad proxies": {
			envPeerHash:       "0123456789AB",
			envPeerName:       "n",
			envTrustedProxies: "not-an-ip",
		},
		"missing proxy url": {envPeerHash: "0123456789AB", envPeerName: "n"},
		"non-http proxy url": {
			envPeerHash: "0123456789AB",
			envPeerName: "n",
			envProxyURL: "socks5://proxy:1080",
		},
		"bad dht bool": {
			envPeerHash:   "0123456789AB",
			envPeerName:   "n",
			envProxyURL:   "http://proxy:4750",
			envNetworkDHT: "maybe",
		},
		"bad dht distribution bool": {
			envPeerHash:        "0123456789AB",
			envPeerName:        "n",
			envProxyURL:        "http://proxy:4750",
			envDHTDistribution: "maybe",
		},
		"bad dht crawling bool": {
			envPeerHash:              "0123456789AB",
			envPeerName:              "n",
			envProxyURL:              "http://proxy:4750",
			envDHTAllowWhileCrawling: "maybe",
		},
		"bad dht indexing bool": {
			envPeerHash:              "0123456789AB",
			envPeerName:              "n",
			envProxyURL:              "http://proxy:4750",
			envDHTAllowWhileIndexing: "maybe",
		},
		"bad dht interval": {
			envPeerHash:                "0123456789AB",
			envPeerName:                "n",
			envProxyURL:                "http://proxy:4750",
			envDHTDistributionInterval: "-1s",
		},
		"malformed dht interval": {
			envPeerHash:                "0123456789AB",
			envPeerName:                "n",
			envProxyURL:                "http://proxy:4750",
			envDHTDistributionInterval: "often",
		},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadNodeConfig(envFrom(env)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPublicSelfTestURLHelpers(t *testing.T) {
	localhost, err := localSelfTestURL("localhost:8091")
	if err != nil {
		t.Fatalf("local host: %v", err)
	}
	if localhost.String() != "http://localhost:8091" {
		t.Fatalf("localhost URL = %s", localhost.String())
	}

	wildcard, err := localSelfTestURL("0.0.0.0:8092")
	if err != nil {
		t.Fatalf("wildcard host: %v", err)
	}
	if wildcard.String() != "http://127.0.0.1:8092" {
		t.Fatalf("wildcard URL = %s", wildcard.String())
	}

	if _, err := localSelfTestURL("bad"); err == nil {
		t.Fatal("bad peer address: expected error")
	}

	for _, raw := range []string{"http://", "%"} {
		if _, err := publicSelfTestURL(
			envFrom(map[string]string{envPublicSelfTestURL: raw}),
			defaultPeerAddr,
		); err == nil {
			t.Fatalf("%q: expected error", raw)
		}
	}
}

func TestParseByteSizeUnits(t *testing.T) {
	cases := map[string]int64{
		"1B":  1,
		"2KB": 2 << 10,
		"3MB": 3 << 20,
		"4GB": 4 << 30,
		"1TB": 1 << 40,
	}
	for raw, want := range cases {
		got, err := parseByteSize(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if got != want {
			t.Errorf("%s = %d, want %d", raw, got, want)
		}
	}
}

func TestParseByteSizeRejects(t *testing.T) {
	for _, raw := range []string{"100", "xxKB", "-1GB"} {
		if _, err := parseByteSize(raw); err == nil {
			t.Errorf("%q: expected error", raw)
		}
	}
}

func TestParseTrustedProxiesAcceptsIPAndCIDR(t *testing.T) {
	nets, err := parseTrustedProxies("10.0.0.1, 192.168.0.0/16, , ::1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(nets) != 3 {
		t.Fatalf("nets = %d, want 3", len(nets))
	}
}

func TestParseTrustedProxiesRejects(t *testing.T) {
	for _, raw := range []string{"999.0.0.1", "10.0.0.0/99"} {
		if _, err := parseTrustedProxies(raw); err == nil {
			t.Errorf("%q: expected error", raw)
		}
	}
}
