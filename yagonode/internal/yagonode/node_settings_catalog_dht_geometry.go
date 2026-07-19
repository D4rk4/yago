package yagonode

import (
	"fmt"
	"strconv"
	"strings"
)

const settingKeyDHTPartitionExponent = "dht.partition_exponent"

func dhtPartitionGeometryDefinition() settingDefinition {
	return settingDefinition{
		key:         settingKeyDHTPartitionExponent,
		title:       "DHT partition exponent",
		description: "Network-wide vertical DHT geometry. Keep 4 for the YaCy freeworld network; changing it to a value from 0 through 8 takes effect after restart and makes routing incompatible with peers using another value.",
		defaultValue: func(config nodeConfig) string {
			return strconv.Itoa(config.DHT.PartitionExponent)
		},
		normalize: normalizeDHTPartitionExponent,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.DHT.PartitionExponent, _ = strconv.Atoi(value)

			return config
		},
	}
}

func normalizeDHTPartitionExponent(raw string) (string, error) {
	return normalizeBoundedInteger(raw, 0, maxDHTPartitionExponent)
}

func normalizeDHTRedundancy(raw string) (string, error) {
	return normalizeBoundedInteger(raw, 1, maxDHTRedundancy)
}

func normalizeDHTMinimumPeerAge(raw string) (string, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < -1 {
		return "", fmt.Errorf("value must be an integer greater than or equal to -1")
	}

	return strconv.Itoa(value), nil
}

func normalizeBoundedInteger(raw string, minimum, maximum int) (string, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < minimum || value > maximum {
		return "", fmt.Errorf("value must be an integer between %d and %d", minimum, maximum)
	}

	return strconv.Itoa(value), nil
}
