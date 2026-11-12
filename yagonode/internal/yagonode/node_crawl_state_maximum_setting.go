package yagonode

const settingKeyCrawlerNodeStateMaximumBytes = "crawler.node_state_max_bytes"

func crawlStateMaximumDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyCrawlerNodeStateMaximumBytes,
			title:       "Node crawl-state soft ceiling",
			description: "Auto uses 10% of the storage quota, capped by measured free space after the node reserve. Enter a byte size to override it, or 0B to disable the ceiling. Lifecycle, settlement, recovery, and migration writes continue.",
			defaultValue: func(config nodeConfig) string {
				if config.Crawl.StateMaximumAutomatic {
					return "auto"
				}

				return formatByteSize(config.Crawl.StateMaximumBytes)
			},
			normalize: normalizeCrawlerNodeStateMaximum,
			apply: func(config nodeConfig, value string) nodeConfig {
				policy, _ := parseCrawlerNodeStateMaximum(value)
				config.Crawl.StateMaximumBytes = policy.MaximumBytes
				config.Crawl.StateMaximumAutomatic = policy.Automatic

				return config
			},
			applyLive: func(toggles *runtimeToggles, value string) {
				policy, _ := parseCrawlerNodeStateMaximum(value)
				toggles.ApplyCrawlerNodeStateMaximum(policy)
			},
		},
	}
}

func (t *runtimeToggles) SetCrawlerNodeStateMaximumSink(sink func(crawlStateMaximumPolicy)) {
	if t != nil && sink != nil {
		t.crawlerNodeStateMaximum.Store(sink)
	}
}

func (t *runtimeToggles) ApplyCrawlerNodeStateMaximum(policy crawlStateMaximumPolicy) {
	if t == nil {
		return
	}
	if sink, ok := t.crawlerNodeStateMaximum.Load().(func(crawlStateMaximumPolicy)); ok {
		sink(policy)
	}
}
