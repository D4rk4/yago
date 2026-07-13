package yagonode

import (
	"context"
	"fmt"
	"strings"
)

const (
	settingKeyWebFallbackPrivacy       = "web.fallback.privacy"
	settingKeyLegacyWebFallbackTrigger = "web.fallback.trigger"
	webFallbackSettingPrefix           = "v2:"
	webFallbackSettingEnvironment      = webFallbackSettingPrefix + "environment"
)

type webFallbackOverrideStore interface {
	Set(context.Context, string, string) error
}

func initializeWebFallbackOverride(
	ctx context.Context,
	store webFallbackOverrideStore,
	config nodeConfig,
	overrides map[string]string,
) error {
	if err := migrateLegacyWebFallbackOverride(ctx, store, config, overrides); err != nil {
		return err
	}
	if _, found := overrides[settingKeyWebFallbackPrivacy]; found {
		return nil
	}
	if err := store.Set(
		ctx,
		settingKeyWebFallbackPrivacy,
		webFallbackSettingEnvironment,
	); err != nil {
		return fmt.Errorf("initialize web fallback setting: %w", err)
	}
	overrides[settingKeyWebFallbackPrivacy] = webFallbackSettingEnvironment

	return nil
}

func migrateLegacyWebFallbackOverride(
	ctx context.Context,
	store webFallbackOverrideStore,
	config nodeConfig,
	overrides map[string]string,
) error {
	rawPrivacy, privacyStored := overrides[settingKeyWebFallbackPrivacy]
	rawTrigger, triggerStored := overrides[settingKeyLegacyWebFallbackTrigger]
	if privacyStored {
		if _, authoritative, _ := decodeWebFallbackSetting(rawPrivacy); authoritative {
			return nil
		}
	}
	if !privacyStored && !triggerStored {
		return nil
	}
	privacy := storedWebFallbackPrivacy(config.WebFallback.Privacy, overrides)
	trigger := config.WebFallback.Trigger
	if triggerStored {
		parsed, err := loadWebFallbackTrigger(func(string) string { return rawTrigger })
		if err == nil {
			trigger = parsed
		} else {
			trigger = webFallbackTriggerMiss
		}
	}
	effective := effectiveWebFallbackPrivacy(webFallbackConfig{
		Privacy: privacy,
		Trigger: trigger,
	})
	encoded := encodeWebFallbackSetting(effective)
	if err := store.Set(ctx, settingKeyWebFallbackPrivacy, encoded); err != nil {
		return fmt.Errorf("migrate web fallback setting: %w", err)
	}
	overrides[settingKeyWebFallbackPrivacy] = encoded

	return nil
}

func storedWebFallbackPrivacy(
	fallback webFallbackPrivacy,
	overrides map[string]string,
) webFallbackPrivacy {
	raw, found := overrides[settingKeyWebFallbackPrivacy]
	if !found {
		return fallback
	}
	privacy, err := loadWebFallbackPrivacy(func(string) string { return raw }, false)
	if err != nil {
		return fallback
	}

	return privacy
}

func encodeWebFallbackSetting(mode webFallbackPrivacy) string {
	return webFallbackSettingPrefix + string(mode)
}

func decodeWebFallbackSetting(
	raw string,
) (webFallbackPrivacy, bool, bool) {
	if raw == webFallbackSettingEnvironment {
		return "", true, true
	}
	if !strings.HasPrefix(raw, webFallbackSettingPrefix) {
		return "", false, false
	}
	mode, err := loadWebFallbackPrivacy(
		func(string) string { return strings.TrimPrefix(raw, webFallbackSettingPrefix) },
		false,
	)
	if err != nil {
		return webFallbackPrivacyDisabled, true, false
	}

	return mode, true, false
}
