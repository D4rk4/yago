package yagonode

import "testing"

func TestCrawlerNodeStateMaximumDefaultFollowsStorageQuota(t *testing.T) {
	config, err := loadCrawlConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load crawl config: %v", err)
	}
	if !config.StateMaximumAutomatic || config.StateMaximumBytes != (1<<30)/10 {
		t.Fatalf(
			"default automatic node crawl state maximum = %d, automatic=%t",
			config.StateMaximumBytes,
			config.StateMaximumAutomatic,
		)
	}
	quotaDerived, err := loadCrawlConfig(func(name string) string {
		if name == envStorageQuota {
			return "160GB"
		}

		return ""
	})
	if err != nil || !quotaDerived.StateMaximumAutomatic ||
		quotaDerived.StateMaximumBytes != 16<<30 {
		t.Fatalf(
			"quota-derived node crawl state maximum = %d, automatic=%t, %v",
			quotaDerived.StateMaximumBytes,
			quotaDerived.StateMaximumAutomatic,
			err,
		)
	}
}

func TestCrawlerNodeStateMaximumAcceptsExplicitOverrideAndDisable(t *testing.T) {
	override, err := loadCrawlConfig(func(name string) string {
		if name == envCrawlerNodeStateMaximumBytes {
			return "8GB"
		}

		return ""
	})
	if err != nil || override.StateMaximumBytes != 8<<30 || override.StateMaximumAutomatic {
		t.Fatalf(
			"override node crawl state maximum = %d, automatic=%t, %v",
			override.StateMaximumBytes,
			override.StateMaximumAutomatic,
			err,
		)
	}
	disabled, err := loadCrawlConfig(func(name string) string {
		if name == envCrawlerNodeStateMaximumBytes {
			return "0"
		}

		return ""
	})
	if err != nil || disabled.StateMaximumBytes != 0 || disabled.StateMaximumAutomatic {
		t.Fatalf("disabled node crawl state maximum = %d, %v", disabled.StateMaximumBytes, err)
	}
	if _, err := loadCrawlConfig(func(name string) string {
		if name == envCrawlerNodeStateMaximumBytes {
			return "invalid"
		}

		return ""
	}); err == nil {
		t.Fatal("invalid node crawl state maximum accepted")
	}
	if _, err := loadCrawlerNodeStateMaximum(func(name string) string {
		if name == envCrawlerNodeStateMaximumBytes {
			return "auto"
		}
		if name == envStorageQuota {
			return "invalid"
		}

		return ""
	}); err == nil {
		t.Fatal("automatic node crawl state maximum accepted an invalid storage quota")
	}
}

func TestCrawlerNodeStateMaximumSettingAppliesLive(t *testing.T) {
	config, err := loadCrawlConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load crawl config: %v", err)
	}
	definition := indexSettingDefinitions()[settingKeyCrawlerNodeStateMaximumBytes]
	if definition.restartRequired() {
		t.Fatal("node crawl state maximum does not apply live")
	}
	automatic, err := definition.normalize("AUTO")
	if err != nil || automatic != "auto" {
		t.Fatalf("normalized automatic node crawl state maximum = %q, %v", automatic, err)
	}
	if _, err := definition.normalize("invalid"); err == nil {
		t.Fatal("invalid node crawl state maximum was accepted")
	}
	automaticConfig := definition.apply(nodeConfig{Crawl: config}, automatic)
	if !automaticConfig.Crawl.StateMaximumAutomatic {
		t.Fatal("auto did not select a quota-derived node crawl state maximum")
	}
	normalized, err := definition.normalize("6GB")
	if err != nil || normalized != "6GB" {
		t.Fatalf("normalized node crawl state maximum = %q, %v", normalized, err)
	}
	disabledValue, err := definition.normalize("0")
	if err != nil || disabledValue != "0B" {
		t.Fatalf("normalized disabled node crawl state maximum = %q, %v", disabledValue, err)
	}
	effective := definition.apply(nodeConfig{Crawl: config}, normalized)
	if effective.Crawl.StateMaximumBytes != 6<<30 {
		t.Fatalf("applied node crawl state maximum = %d", effective.Crawl.StateMaximumBytes)
	}
	toggles := newRuntimeToggles(nodeConfig{Crawl: config})
	var live crawlStateMaximumPolicy
	toggles.SetCrawlerNodeStateMaximumSink(func(value crawlStateMaximumPolicy) { live = value })
	definition.applyLive(toggles, automatic)
	if !live.Automatic {
		t.Fatal("live auto setting did not select a quota-derived node crawl state maximum")
	}
	definition.applyLive(toggles, normalized)
	if live.Automatic || live.MaximumBytes != 6<<30 {
		t.Fatalf("live node crawl state maximum = %+v", live)
	}
	var unavailable *runtimeToggles
	unavailable.ApplyCrawlerNodeStateMaximum(live)
}
