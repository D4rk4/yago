package yagonode

import (
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagoproto"
)

const (
	settingKeyNetworkAuthenticationMode   = "network.authentication.mode"
	settingKeyNetworkAuthenticationSecret = "network.authentication.secret"
)

func networkAuthenticationSettingDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyNetworkAuthenticationSecret,
			title:       "Shared network secret",
			description: "Shared secret used to authenticate YaCy peer protocol requests on a controlled private network. Changes take effect after a restart.",
			defaultValue: func(config nodeConfig) string {
				return config.NetworkAuthenticationSecret
			},
			normalize: normalizeNetworkAuthenticationSecret,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.NetworkAuthenticationSecret = value

				return config
			},
			sensitive: true,
		},
		{
			key:         settingKeyNetworkAuthenticationMode,
			title:       "YaCy network authentication",
			description: "Authentication mode for YaCy peer protocol traffic. Controlled private networks require the same shared secret on every peer. Changes take effect after a restart.",
			options: []settingOption{
				{value: string(yagoproto.NetworkAuthenticationUncontrolled), label: "Uncontrolled"},
				{
					value: string(yagoproto.NetworkAuthenticationSaltedMagic),
					label: "Salted magic (private network)",
				},
			},
			defaultValue: func(config nodeConfig) string {
				return string(config.NetworkAuthenticationMode)
			},
			normalize: normalizeNetworkAuthenticationMode,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.NetworkAuthenticationMode = yagoproto.NetworkAuthenticationMode(value)

				return config
			},
		},
	}
}

func normalizeNetworkAuthenticationMode(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	switch yagoproto.NetworkAuthenticationMode(value) {
	case yagoproto.NetworkAuthenticationUncontrolled,
		yagoproto.NetworkAuthenticationSaltedMagic:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported network authentication mode")
	}
}

func normalizeNetworkAuthenticationSecret(raw string) (string, error) {
	if err := validateConfiguredNetworkAuthenticationSecret(raw); err != nil {
		return "", err
	}

	return raw, nil
}
