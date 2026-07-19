package yagonode

import (
	"net/url"
	"strconv"
	"strings"
)

const (
	settingKeyNetworkAdvertisePort  = "network.advertise.port"
	settingKeyNetworkPublicSelfTest = "network.public_self_test_url"
)

func networkAdvertisementDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyNetworkAdvertisePort,
			title:       "Advertised port",
			description: "Public peer-protocol port announced to the swarm. Empty derives it from the peer listener after the next restart.",
			defaultValue: func(config nodeConfig) string {
				if !config.AdvertisePortPinned {
					return ""
				}

				return strconv.Itoa(config.AdvertisePort)
			},
			normalize: normalizeOptionalAdvertisePort,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.AdvertisePortPinned = value != ""
				if value == "" {
					config.AdvertisePort = 0

					return config
				}
				config.AdvertisePort, _ = strconv.Atoi(value)

				return config
			},
		},
		{
			key:         settingKeyNetworkPublicSelfTest,
			title:       "Public peer self-test URL",
			description: "Absolute public HTTP(S) base used to verify this node before DHT distribution. Empty derives a local peer URL after the next restart.",
			defaultValue: func(config nodeConfig) string {
				if !config.SelfTestURLPinned || config.PublicSelfTestURL == nil {
					return ""
				}

				return config.PublicSelfTestURL.String()
			},
			normalize: normalizeOptionalPublicSelfTestURL,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.SelfTestURLPinned = value != ""
				if value == "" {
					config.PublicSelfTestURL = nil

					return config
				}
				config.PublicSelfTestURL, _ = url.Parse(value)

				return config
			},
		},
	}
}

func normalizeOptionalAdvertisePort(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	port, err := parseBindPort(value)
	if err != nil {
		return "", err
	}

	return strconv.Itoa(port), nil
}
