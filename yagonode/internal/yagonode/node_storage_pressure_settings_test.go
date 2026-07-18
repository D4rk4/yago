package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestStoragePressureConfigDefaultsOverridesAndFailures(t *testing.T) {
	data, err := loadConfiguredNodeData(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load default node data: %v", err)
	}
	if data.reservedFreeBytes != 1<<30 || data.pressureRecoveryBytes != 256<<20 {
		t.Fatalf("default node storage pressure = %+v", data)
	}
	overrides := map[string]string{
		envDataDir: "state", envStorageReservedFree: "2GB", envStorageHysteresis: "64MB",
	}
	data, err = loadConfiguredNodeData(func(key string) string { return overrides[key] })
	if err != nil {
		t.Fatalf("load node pressure overrides: %v", err)
	}
	if data.reservedFreeBytes != 2<<30 || data.pressureRecoveryBytes != 64<<20 {
		t.Fatalf("node pressure overrides = %+v", data)
	}
	for _, key := range []string{envStorageReservedFree, envStorageHysteresis} {
		values := map[string]string{key: "invalid"}
		if _, err := loadConfiguredNodeData(
			func(name string) string { return values[name] },
		); err == nil {
			t.Fatalf("invalid %s accepted", key)
		}
	}
	if _, err := parseByteSize("9223372036854775807TB"); err == nil {
		t.Fatal("overflowing byte size accepted")
	}
}

func TestCrawlerStoragePressureConfigDefaultsOverridesAndFailures(t *testing.T) {
	config, err := loadCrawlConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load default crawl config: %v", err)
	}
	if config.StorageReservedFreeBytes != 1<<30 ||
		config.StoragePressureRecoveryBytes != 256<<20 {
		t.Fatalf("default crawler storage pressure = %+v", config)
	}
	overrides := map[string]string{
		envCrawlerStorageReservedFree: "3GB", envCrawlerStorageHysteresis: "32MB",
	}
	config, err = loadCrawlConfig(func(key string) string { return overrides[key] })
	if err != nil {
		t.Fatalf("load crawler pressure overrides: %v", err)
	}
	if config.StorageReservedFreeBytes != 3<<30 ||
		config.StoragePressureRecoveryBytes != 32<<20 {
		t.Fatalf("crawler pressure overrides = %+v", config)
	}
	for _, key := range []string{envCrawlerStorageReservedFree, envCrawlerStorageHysteresis} {
		values := map[string]string{key: "invalid"}
		if _, err := loadCrawlConfig(func(name string) string { return values[name] }); err == nil {
			t.Fatalf("invalid %s accepted", key)
		}
	}
}

func TestStoragePressureSettingDefinitionsApplyAndUpdateLive(t *testing.T) {
	definitions := map[string]settingDefinition{}
	for _, definition := range storagePressureDefinitions() {
		definitions[definition.key] = definition
	}
	config := nodeConfig{
		StorageReservedFreeBytes:     1 << 30,
		StoragePressureRecoveryBytes: 256 << 20,
		Crawl: crawlConfig{
			StorageReservedFreeBytes:     2 << 30,
			StoragePressureRecoveryBytes: 128 << 20,
		},
	}
	toggles := newRuntimeToggles(config)
	var nodePolicy, crawlerPolicy yagocrawlcontract.StoragePressurePolicy
	toggles.SetStoragePressureSink(func(policy yagocrawlcontract.StoragePressurePolicy) {
		nodePolicy = policy
	})
	toggles.SetCrawlerStoragePressureSink(func(policy yagocrawlcontract.StoragePressurePolicy) {
		crawlerPolicy = policy
	})
	cases := []struct {
		key   string
		value string
		want  int64
	}{
		{settingKeyStorageReservedFree, "4GB", 4 << 30},
		{settingKeyStoragePressureHysteresis, "16MB", 16 << 20},
		{settingKeyCrawlerStorageReservedFree, "5GB", 5 << 30},
		{settingKeyCrawlerStorageHysteresis, "8MB", 8 << 20},
	}
	for _, test := range cases {
		definition := definitions[test.key]
		if definition.defaultValue(config) == "" {
			t.Fatalf("%s has empty default", test.key)
		}
		normalized, err := definition.normalize(test.value)
		if err != nil {
			t.Fatalf("normalize %s: %v", test.key, err)
		}
		config = definition.apply(config, normalized)
		definition.applyLive(toggles, normalized)
	}
	if config.StorageReservedFreeBytes != 4<<30 ||
		config.StoragePressureRecoveryBytes != 16<<20 ||
		config.Crawl.StorageReservedFreeBytes != 5<<30 ||
		config.Crawl.StoragePressureRecoveryBytes != 8<<20 {
		t.Fatalf("applied pressure config = %+v", config)
	}
	if nodePolicy.ReservedFreeBytes != 4<<30 ||
		nodePolicy.RecoveryHysteresisBytes != 16<<20 ||
		crawlerPolicy.ReservedFreeBytes != 5<<30 ||
		crawlerPolicy.RecoveryHysteresisBytes != 8<<20 {
		t.Fatalf("live policies = node:%+v crawler:%+v", nodePolicy, crawlerPolicy)
	}
	if normalized, err := normalizeStoragePressureSize("0B"); err != nil || normalized != "0B" {
		t.Fatalf("normalize zero = %q, %v", normalized, err)
	}
	if _, err := normalizeStoragePressureSize("bad"); err == nil {
		t.Fatal("invalid storage pressure setting accepted")
	}
	policy := storagePressurePolicy(-1, -1)
	if policy.ReservedFreeBytes != 0 || policy.RecoveryHysteresisBytes != 0 {
		t.Fatalf("negative policy = %+v", policy)
	}
	if nodeStoragePressurePolicy(config).ReservedFreeBytes != 4<<30 ||
		crawlerStoragePressurePolicy(config.Crawl).ReservedFreeBytes != 5<<30 {
		t.Fatal("config pressure policy conversion failed")
	}
}

func TestStoragePressureRuntimeTogglesAreNilSafeAndAttachToCrawler(t *testing.T) {
	var nilToggles *runtimeToggles
	nilToggles.SetStoragePressureSink(func(yagocrawlcontract.StoragePressurePolicy) {})
	nilToggles.ApplyStorageReservedFree(1)
	nilToggles.ApplyStoragePressureRecovery(1)
	nilToggles.SetCrawlerStoragePressureSink(func(yagocrawlcontract.StoragePressurePolicy) {})
	nilToggles.ApplyCrawlerStorageReservedFree(1)
	nilToggles.ApplyCrawlerStoragePressureRecovery(1)

	toggles := newRuntimeToggles(nodeConfig{})
	toggles.ApplyStorageReservedFree(1)
	toggles.ApplyStoragePressureRecovery(2)
	toggles.ApplyCrawlerStorageReservedFree(3)
	toggles.ApplyCrawlerStoragePressureRecovery(4)
	runtime := liveCrawlRuntime(t)
	attachCrawlRuntimeSettings(runtime, toggles)
	toggles.ApplyCrawlerStorageReservedFree(6 << 20)
	toggles.ApplyCrawlerStoragePressureRecovery(2 << 20)
	policy := runtime.controlRegistry().StoragePressurePolicy()
	if policy.ReservedFreeBytes != 6<<20 || policy.RecoveryHysteresisBytes != 2<<20 {
		t.Fatalf("attached crawler pressure policy = %+v", policy)
	}
}
