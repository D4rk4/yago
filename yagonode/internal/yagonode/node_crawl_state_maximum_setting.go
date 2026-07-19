package yagonode

const settingKeyCrawlerNodeStateMaximumBytes = "crawler.node_state_max_bytes"

func crawlStateMaximumDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyCrawlerNodeStateMaximumBytes,
			title:       "Node crawl-state soft ceiling",
			description: "Stop accepting new crawl orders when crawlbroker.db reaches this physical size. Lifecycle, settlement, recovery, and startup migration writes continue. Startup attempts to reclaim unused bbolt pages before opening the file; the value is a soft admission boundary, not a filesystem quota.",
			defaultValue: func(config nodeConfig) string {
				return formatByteSize(config.Crawl.StateMaximumBytes)
			},
			normalize: normalizeStoragePressureSize,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.Crawl.StateMaximumBytes, _ = parseByteSize(value)

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				maximumBytes, _ := parseByteSize(value)
				toggles.ApplyCrawlerNodeStateMaximum(maximumBytes)
			},
		},
	}
}

func (t *runtimeToggles) SetCrawlerNodeStateMaximumSink(sink func(int64)) {
	if t != nil && sink != nil {
		t.crawlerNodeStateMaximum.Store(sink)
	}
}

func (t *runtimeToggles) ApplyCrawlerNodeStateMaximum(maximumBytes int64) {
	if t == nil {
		return
	}
	if sink, ok := t.crawlerNodeStateMaximum.Load().(func(int64)); ok {
		sink(maximumBytes)
	}
}
