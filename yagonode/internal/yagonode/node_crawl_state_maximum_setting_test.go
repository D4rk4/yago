package yagonode

import "testing"

func TestCrawlerNodeStateMaximumBootstrapAndAdminApplyLive(t *testing.T) {
	config, err := loadCrawlConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("load crawl config: %v", err)
	}
	if config.StateMaximumBytes != 4<<30 {
		t.Fatalf("default node crawl state maximum = %d", config.StateMaximumBytes)
	}
	override, err := loadCrawlConfig(func(name string) string {
		if name == envCrawlerNodeStateMaximumBytes {
			return "8GB"
		}

		return ""
	})
	if err != nil || override.StateMaximumBytes != 8<<30 {
		t.Fatalf("override node crawl state maximum = %d, %v", override.StateMaximumBytes, err)
	}
	disabled, err := loadCrawlConfig(func(name string) string {
		if name == envCrawlerNodeStateMaximumBytes {
			return "0"
		}

		return ""
	})
	if err != nil || disabled.StateMaximumBytes != 0 {
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
	definition := indexSettingDefinitions()[settingKeyCrawlerNodeStateMaximumBytes]
	if definition.restartRequired() {
		t.Fatal("node crawl state maximum does not apply live")
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
	var live int64
	toggles.SetCrawlerNodeStateMaximumSink(func(value int64) { live = value })
	definition.applyLive(toggles, normalized)
	if live != 6<<30 {
		t.Fatalf("live node crawl state maximum = %d", live)
	}
	var unavailable *runtimeToggles
	unavailable.ApplyCrawlerNodeStateMaximum(6 << 30)
}
