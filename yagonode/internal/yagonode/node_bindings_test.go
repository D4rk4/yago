package yagonode

import (
	"net"
	"net/url"
	"testing"
)

func TestSplitBindAddr(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		addr     string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{addr: ":8090", wantHost: "", wantPort: 8090},
		{addr: "127.0.0.1:9090", wantHost: "127.0.0.1", wantPort: 9090},
		{addr: "0.0.0.0:80", wantHost: "0.0.0.0", wantPort: 80},
		{addr: "example.com:80", wantErr: true},
		{addr: ":0", wantErr: true},
		{addr: ":70000", wantErr: true},
		{addr: "nope", wantErr: true},
	} {
		host, port, err := splitBindAddr(tc.addr)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("splitBindAddr(%q) = (%q,%d), want error", tc.addr, host, port)
			}

			continue
		}
		if err != nil {
			t.Fatalf("splitBindAddr(%q): %v", tc.addr, err)
		}
		if host != tc.wantHost || port != tc.wantPort {
			t.Fatalf(
				"splitBindAddr(%q) = (%q,%d), want (%q,%d)",
				tc.addr,
				host,
				port,
				tc.wantHost,
				tc.wantPort,
			)
		}
	}
}

func TestApplyBindOverridesReplacesListenAddress(t *testing.T) {
	t.Parallel()

	config := applyBindOverrides(
		nodeConfig{
			PeerAddr: ":8090", OpsAddr: ":9090", PublicAddr: ":8080",
			Crawl: crawlConfig{ListenAddr: ":9091"},
		},
		map[string]string{
			bindKeyPeer:    "127.0.0.1:8091",
			bindKeyOps:     "0.0.0.0:9191",
			bindKeyPublic:  "127.0.0.1:8081",
			bindKeyCrawler: "127.0.0.1:9092",
		},
	)
	if config.PeerAddr != "127.0.0.1:8091" {
		t.Fatalf("PeerAddr = %q, want 127.0.0.1:8091", config.PeerAddr)
	}
	if config.OpsAddr != "0.0.0.0:9191" {
		t.Fatalf("OpsAddr = %q, want 0.0.0.0:9191", config.OpsAddr)
	}
	if config.PublicAddr != "127.0.0.1:8081" {
		t.Fatalf("PublicAddr = %q, want 127.0.0.1:8081", config.PublicAddr)
	}
	if config.Crawl.ListenAddr != "127.0.0.1:9092" {
		t.Fatalf("Crawl.ListenAddr = %q, want 127.0.0.1:9092", config.Crawl.ListenAddr)
	}
}

func TestApplyBindOverridesIgnoresMalformed(t *testing.T) {
	t.Parallel()

	config := applyBindOverrides(
		nodeConfig{PeerAddr: ":8090"},
		map[string]string{bindKeyPeer: "garbage"},
	)
	if config.PeerAddr != ":8090" {
		t.Fatalf("PeerAddr = %q, want the environment default :8090", config.PeerAddr)
	}
}

func TestApplyBindOverridesDisablesOptionalListeners(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		key      string
		disabled func(nodeConfig) bool
	}{
		{
			name: "public search", key: bindKeyPublic,
			disabled: func(config nodeConfig) bool { return config.PublicAddr == "" },
		},
		{
			name: "crawler exchange", key: bindKeyCrawler,
			disabled: func(config nodeConfig) bool { return config.Crawl.ListenAddr == "" },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := applyBindOverrides(
				nodeConfig{
					PeerAddr: ":8090", OpsAddr: ":9090", PublicAddr: ":8080",
					Crawl: crawlConfig{ListenAddr: ":9091"},
				},
				map[string]string{tc.key: disabledBindOverride},
			)
			if !tc.disabled(config) {
				t.Fatalf("%s remains enabled: %+v", tc.name, config)
			}
		})
	}

	config := applyBindOverrides(
		nodeConfig{PeerAddr: ":8090", OpsAddr: ":9090", PublicAddr: ":8080"},
		map[string]string{bindKeyPeer: disabledBindOverride},
	)
	if config.PeerAddr != ":8090" {
		t.Fatalf("PeerAddr = %q, want environment value", config.PeerAddr)
	}
}

func TestApplyBindOverridesRealignsPeerDerivedValues(t *testing.T) {
	t.Parallel()

	config := applyBindOverrides(
		nodeConfig{PeerAddr: ":8090", AdvertisePort: 8090},
		map[string]string{bindKeyPeer: "127.0.0.1:8091"},
	)
	if config.AdvertisePort != 8091 {
		t.Fatalf("AdvertisePort = %d, want the re-derived 8091", config.AdvertisePort)
	}
	if config.PublicSelfTestURL == nil || config.PublicSelfTestURL.Port() != "8091" {
		t.Fatalf("PublicSelfTestURL = %v, want port 8091", config.PublicSelfTestURL)
	}
}

func TestApplyBindOverridesKeepsPinnedPeerDerivedValues(t *testing.T) {
	t.Parallel()

	pinned := &url.URL{Scheme: "https", Host: "node.example:9000"}
	config := applyBindOverrides(
		nodeConfig{
			PeerAddr:            ":8090",
			AdvertisePort:       7000,
			AdvertisePortPinned: true,
			PublicSelfTestURL:   pinned,
			SelfTestURLPinned:   true,
		},
		map[string]string{bindKeyPeer: "127.0.0.1:8091"},
	)
	if config.AdvertisePort != 7000 {
		t.Fatalf("AdvertisePort = %d, want the pinned 7000", config.AdvertisePort)
	}
	if config.PublicSelfTestURL != pinned {
		t.Fatalf("PublicSelfTestURL = %v, want the pinned URL unchanged", config.PublicSelfTestURL)
	}
}

func TestValidateNodeBinds(t *testing.T) {
	t.Parallel()

	if err := validateNodeBinds(
		nodeConfig{
			PeerAddr: ":8090", OpsAddr: "127.0.0.1:9090", PublicAddr: ":8080",
			Crawl: crawlConfig{ListenAddr: "127.0.0.1:9091"},
		},
	); err != nil {
		t.Fatalf("valid binds rejected: %v", err)
	}
	if err := validateNodeBinds(
		nodeConfig{
			PeerAddr: ":8090", OpsAddr: ":9090", PublicAddr: "",
			Crawl: crawlConfig{ListenAddr: ""},
		},
	); err != nil {
		t.Fatalf("disabled (empty) public bind rejected: %v", err)
	}
	if err := validateNodeBinds(nodeConfig{PeerAddr: "bogus", OpsAddr: ":9090"}); err == nil {
		t.Fatal("malformed peer bind accepted")
	}
	if err := validateNodeBinds(
		nodeConfig{PeerAddr: ":8090", OpsAddr: ":9090", PublicAddr: "bogus"},
	); err == nil {
		t.Fatal("malformed public bind accepted")
	}
	if err := validateNodeBinds(nodeConfig{
		PeerAddr: ":8090", OpsAddr: ":9090", Crawl: crawlConfig{ListenAddr: "bogus"},
	}); err == nil {
		t.Fatal("malformed crawler bind accepted")
	}
}

func TestDiscoverBindAddressesIncludesAllAndLoopback(t *testing.T) {
	t.Parallel()

	addrs, err := discoverBindAddresses(func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("127.0.0.1")},
			&net.IPNet{IP: net.ParseIP("192.168.1.5")},
			&net.IPNet{IP: net.ParseIP("169.254.1.1")},
		}, nil
	})
	if err != nil {
		t.Fatalf("discoverBindAddresses: %v", err)
	}

	byHost := map[string]string{}
	for _, addr := range addrs {
		byHost[addr.host] = addr.label
	}
	if byHost[""] != bindAllInterfacesLabel {
		t.Fatal("all-interfaces option missing")
	}
	if _, ok := byHost["192.168.1.5"]; !ok {
		t.Fatal("private interface address missing")
	}
	if _, ok := byHost["169.254.1.1"]; ok {
		t.Fatal("link-local address should be excluded")
	}
	if got := byHost["127.0.0.1"]; got == "" || !contains(got, "loopback") {
		t.Fatalf("loopback label = %q, want it to mention loopback", got)
	}
}

func TestDiscoverBindAddressesPropagatesError(t *testing.T) {
	t.Parallel()

	_, err := discoverBindAddresses(func() ([]net.Addr, error) {
		return nil, net.UnknownNetworkError("boom")
	})
	if err == nil {
		t.Fatal("expected error from the interface source")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}

	return false
}
