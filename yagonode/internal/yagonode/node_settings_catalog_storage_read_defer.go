package yagonode

import (
	"fmt"
	"strings"
	"time"
)

const settingKeyStorageReadDefer = "storage.read_defer"

func storageReadDeferDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyStorageReadDefer,
			title:       "Interactive read priority budget",
			description: "Maximum time each storage write yields to active interactive reads. Zero uses the 50ms engine default; a negative duration disables yielding.",
			defaultValue: func(config nodeConfig) string {
				return config.StorageReadDefer.String()
			},
			normalize: normalizeStorageReadDefer,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.StorageReadDefer, _ = time.ParseDuration(value)

				return config
			},
		},
	}
}

func normalizeStorageReadDefer(raw string) (string, error) {
	budget, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid storage read priority budget: %w", err)
	}

	return budget.String(), nil
}
