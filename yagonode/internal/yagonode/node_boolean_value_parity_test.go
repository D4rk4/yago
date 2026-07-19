package yagonode

import "testing"

func TestBooleanEnvironmentAndAdminShareCanonicalValues(t *testing.T) {
	definitions := indexSettingDefinitions()
	controls := []struct {
		environment string
		setting     string
		fallback    bool
	}{
		{envMetricsEnabled, "metrics.enabled", true},
		{envNetworkDHT, "dht.enabled", true},
		{envCrawlerAllowPrivateNetworks, settingKeyCrawlerAllowPrivateNetworks, false},
	}
	for _, control := range controls {
		definition := definitions[control.setting]
		for _, raw := range []string{" true ", " FALSE ", "1", "0"} {
			bootstrap, bootstrapErr := boolEnv(
				func(name string) string {
					if name == control.environment {
						return raw
					}
					return ""
				},
				control.environment,
				control.fallback,
			)
			admin, adminErr := definition.normalize(raw)
			if bootstrapErr != nil || adminErr != nil || admin != formatSettingBool(bootstrap) {
				t.Errorf(
					"%s/%s value %q = %t/%q, errors %v/%v",
					control.environment,
					control.setting,
					raw,
					bootstrap,
					admin,
					bootstrapErr,
					adminErr,
				)
			}
		}
	}
}

func TestBooleanEnvironmentAndAdminRejectTheSameInvalidValues(t *testing.T) {
	definitions := indexSettingDefinitions()
	for _, control := range []struct {
		environment string
		setting     string
	}{
		{envMetricsEnabled, "metrics.enabled"},
		{envNetworkDHT, "dht.enabled"},
		{envCrawlerAllowPrivateNetworks, settingKeyCrawlerAllowPrivateNetworks},
	} {
		for _, raw := range []string{" enabled ", "disabled", "yes", "2"} {
			_, bootstrapErr := boolEnv(
				func(name string) string {
					if name == control.environment {
						return raw
					}
					return ""
				},
				control.environment,
				false,
			)
			_, adminErr := definitions[control.setting].normalize(raw)
			if bootstrapErr == nil || adminErr == nil {
				t.Errorf(
					"%s/%s accepted invalid value %q: errors %v/%v",
					control.environment,
					control.setting,
					raw,
					bootstrapErr,
					adminErr,
				)
			}
		}
	}
}
