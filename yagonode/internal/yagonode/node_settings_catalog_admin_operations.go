package yagonode

const settingKeyAdminRestartControls = "admin.restart_controls.enabled"

func adminOperationsDefinitions() []settingDefinition {
	return []settingDefinition{
		{
			key:         settingKeyAdminRestartControls,
			title:       "Restart controls",
			description: "Offer node and crawler restart actions in the Admin console after the next node restart. The mandatory first-run setup restart is unaffected.",
			options:     boolSettingOptions(),
			defaultValue: func(config nodeConfig) string {
				return formatSettingBool(config.AdminRestartEnabled)
			},
			normalize: normalizeSettingBool,
			apply: func(config nodeConfig, value string) nodeConfig {
				config.AdminRestartEnabled = value == settingBoolTrue

				return config
			},
		},
	}
}
