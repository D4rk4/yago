package yagonode

import "testing"

func TestLoadNodeConfigStorageDeferFsync(t *testing.T) {
	base := map[string]string{envPeerHash: "0123456789AB", envPeerName: "node"}
	load := func(t *testing.T, value string) nodeConfig {
		t.Helper()
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		if value != "" {
			env[envStorageDeferFsync] = value
		}
		config, err := loadNodeConfig(envFrom(env))
		if err != nil {
			t.Fatalf("load config %q: %v", value, err)
		}

		return config
	}

	if load(t, "").StorageDeferFsync {
		t.Error("StorageDeferFsync defaulted on, want off")
	}
	if !load(t, "true").StorageDeferFsync {
		t.Error("StorageDeferFsync(true) did not enable deferred fsync")
	}
	if load(t, "false").StorageDeferFsync {
		t.Error("StorageDeferFsync(false) enabled deferred fsync")
	}

	env := map[string]string{envStorageDeferFsync: "nonsense"}
	for k, v := range base {
		env[k] = v
	}
	if _, err := loadNodeConfig(envFrom(env)); err == nil {
		t.Fatal("expected error for a malformed defer-fsync flag")
	}
}

func TestStorageDeferFsyncSetting(t *testing.T) {
	t.Parallel()

	def, ok := indexSettingDefinitions()["storage.defer_fsync"]
	if !ok {
		t.Fatal("storage.defer_fsync missing from the catalog")
	}
	if !def.restartRequired() {
		t.Fatal("defer-fsync must be restart-required (no live apply)")
	}
	if got := def.defaultValue(nodeConfig{StorageDeferFsync: true}); got != settingBoolTrue {
		t.Fatalf("default(on) = %q, want %q", got, settingBoolTrue)
	}
	if got := def.defaultValue(nodeConfig{StorageDeferFsync: false}); got != settingBoolFalse {
		t.Fatalf("default(off) = %q, want %q", got, settingBoolFalse)
	}
	if norm, err := def.normalize("true"); err != nil || norm != settingBoolTrue {
		t.Fatalf("normalize(true) = %q %v, want %q", norm, err, settingBoolTrue)
	}
	if _, err := def.normalize("nonsense"); err == nil {
		t.Fatal("normalize must reject a non-boolean value")
	}
	if applied := def.apply(nodeConfig{}, settingBoolTrue); !applied.StorageDeferFsync {
		t.Fatal("apply(true) did not set StorageDeferFsync")
	}
	if applied := def.apply(
		nodeConfig{StorageDeferFsync: true},
		settingBoolFalse,
	); applied.StorageDeferFsync {
		t.Fatal("apply(false) did not clear StorageDeferFsync")
	}
}
