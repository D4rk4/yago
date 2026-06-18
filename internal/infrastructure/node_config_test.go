package infrastructure

import (
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func validConfigEnv() map[string]string {
	return map[string]string{
		EnvPeerHash:      "0123456789AB",
		EnvPeerName:      "node-1",
		EnvAdvertiseHost: "203.0.113.1",
	}
}

func TestLoadNodeConfigDefaults(t *testing.T) {
	config, err := LoadNodeConfig(envFrom(validConfigEnv()), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Hash != yacymodel.Hash("0123456789AB") {
		t.Errorf("hash = %q", config.Hash)
	}
	if config.NetworkName != yacyproto.DefaultNetwork {
		t.Errorf("network = %q, want default", config.NetworkName)
	}
	if config.PeerAddr != defaultPeerAddr {
		t.Errorf("peer addr = %q, want default", config.PeerAddr)
	}
	if config.AdvertisePort != 8090 {
		t.Errorf("advertise port = %d, want peer addr port", config.AdvertisePort)
	}
	if want := filepath.Join(defaultDataDir, storageFileName); config.StoragePath != want {
		t.Errorf("storage path = %q, want %q", config.StoragePath, want)
	}
	if config.StorageQuotaByte != 1<<30 {
		t.Errorf("quota = %d, want 1GB default", config.StorageQuotaByte)
	}
	if !config.Flags.Get(yacymodel.FlagAcceptRemoteIndex) {
		t.Error("expected accept-remote-index flag")
	}
}

func TestLoadNodeConfigOverrides(t *testing.T) {
	env := validConfigEnv()
	env[EnvNetworkName] = "testnet"
	env[EnvPeerAddr] = ":9000"
	env[EnvAdvertisePort] = "443"
	env[EnvDataDir] = "/srv/yacy"
	env[EnvStorageQuota] = "512MB"

	config, err := LoadNodeConfig(envFrom(env), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.NetworkName != "testnet" || config.PeerAddr != ":9000" {
		t.Errorf("overrides not applied: %+v", config)
	}
	if config.AdvertisePort != 443 {
		t.Errorf("advertise port = %d, want 443", config.AdvertisePort)
	}
	if want := filepath.Join("/srv/yacy", storageFileName); config.StoragePath != want {
		t.Errorf("storage path = %q, want %q", config.StoragePath, want)
	}
	if config.StorageQuotaByte != 512<<20 {
		t.Errorf("quota = %d, want 512MB", config.StorageQuotaByte)
	}
}

func TestLoadNodeConfigRequiresAdvertiseHostWhenAnnouncing(t *testing.T) {
	env := validConfigEnv()
	delete(env, EnvAdvertiseHost)

	if _, err := LoadNodeConfig(envFrom(env), true); err == nil {
		t.Fatal("expected error when announcing without advertise host")
	}
	if _, err := LoadNodeConfig(envFrom(env), false); err != nil {
		t.Fatalf("advertise host should be optional when not announcing: %v", err)
	}
}

func TestLoadNodeConfigRejectsMissingAndInvalid(t *testing.T) {
	cases := map[string]func(map[string]string){
		"missing hash":      func(m map[string]string) { delete(m, EnvPeerHash) },
		"invalid hash":      func(m map[string]string) { m[EnvPeerHash] = "short" },
		"missing name":      func(m map[string]string) { delete(m, EnvPeerName) },
		"invalid peer addr": func(m map[string]string) { m[EnvPeerAddr] = "noport" },
		"invalid port":      func(m map[string]string) { m[EnvAdvertisePort] = "x" },
		"negative port":     func(m map[string]string) { m[EnvAdvertisePort] = "-1" },
		"invalid quota":     func(m map[string]string) { m[EnvStorageQuota] = "x" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			env := validConfigEnv()
			mutate(env)
			if _, err := LoadNodeConfig(envFrom(env), false); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
