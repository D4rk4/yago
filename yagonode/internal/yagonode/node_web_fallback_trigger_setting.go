package yagonode

func webFallbackTriggerDefinition() settingDefinition {
	return settingDefinition{
		key:         "web.fallback.trigger",
		title:       "Web search timing",
		description: "Choose whether eligible web search starts after a local and peer miss or alongside local and peer retrieval.",
		options: []settingOption{
			{value: string(webFallbackTriggerMiss), label: "After local and peer miss"},
			{value: string(webFallbackTriggerParallel), label: "Alongside local and peers"},
		},
		defaultValue: func(config nodeConfig) string {
			return string(effectiveWebFallbackTrigger(config.WebFallback.Trigger))
		},
		normalize: func(raw string) (string, error) {
			value, err := loadWebFallbackTrigger(func(string) string { return raw })
			if err != nil {
				return "", err
			}

			return string(value), nil
		},
		apply: func(config nodeConfig, value string) nodeConfig {
			config.WebFallback.Trigger = webFallbackTrigger(value)

			return config
		},
	}
}
