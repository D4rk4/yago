package yagonode

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	settingKeyNetworkName   = "network.name"
	maximumNetworkNameBytes = 128
)

func networkNameDefinitions() []settingDefinition {
	return []settingDefinition{{
		key:   settingKeyNetworkName,
		title: "YaCy network name",
		description: "Exact network unit this node joins. Changing it isolates the node " +
			"from peers on the previous network after restart.",
		defaultValue: func(config nodeConfig) string {
			return config.NetworkName
		},
		normalize: normalizeNetworkName,
		apply: func(config nodeConfig, value string) nodeConfig {
			config.NetworkName = value

			return config
		},
	}}
}

func normalizeNetworkName(raw string) (string, error) {
	value := strings.Trim(raw, " ")
	if value == "" || len(value) > maximumNetworkNameBytes || !utf8.ValidString(value) ||
		strings.ContainsFunc(value, func(character rune) bool {
			return unicode.IsControl(character) || unicode.In(
				character,
				unicode.Cf,
				unicode.Zl,
				unicode.Zp,
			)
		}) {
		return "", fmt.Errorf(
			"value must be one visible line between 1 and %d bytes",
			maximumNetworkNameBytes,
		)
	}

	return value, nil
}
