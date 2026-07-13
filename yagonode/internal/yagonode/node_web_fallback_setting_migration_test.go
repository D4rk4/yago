package yagonode

import (
	"context"
	"errors"
	"testing"
)

type webFallbackMigrationStore struct {
	values map[string]string
	err    error
	calls  int
}

func (s *webFallbackMigrationStore) Set(
	_ context.Context,
	key string,
	value string,
) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[key] = value

	return nil
}

func TestLegacyWebFallbackOverrideMigrationPrecedence(t *testing.T) {
	tests := []struct {
		name      string
		env       webFallbackConfig
		overrides map[string]string
		want      webFallbackPrivacy
	}{
		{
			name: "stored miss overrides parallel environment",
			env: webFallbackConfig{
				Privacy: webFallbackPrivacyEnabled,
				Trigger: webFallbackTriggerParallel,
			},
			overrides: map[string]string{settingKeyLegacyWebFallbackTrigger: "miss"},
			want:      webFallbackPrivacyEnabled,
		},
		{
			name: "stored enabled and parallel override disabled environment",
			env: webFallbackConfig{
				Privacy: webFallbackPrivacyDisabled,
				Trigger: webFallbackTriggerMiss,
			},
			overrides: map[string]string{
				settingKeyWebFallbackPrivacy:       "enabled",
				settingKeyLegacyWebFallbackTrigger: "parallel",
			},
			want: webFallbackPrivacyAlways,
		},
		{
			name: "stored enabled inherits parallel environment once",
			env: webFallbackConfig{
				Privacy: webFallbackPrivacyDisabled,
				Trigger: webFallbackTriggerParallel,
			},
			overrides: map[string]string{settingKeyWebFallbackPrivacy: "enabled"},
			want:      webFallbackPrivacyAlways,
		},
		{
			name: "explicit consent never broadens",
			env: webFallbackConfig{
				Privacy: webFallbackPrivacyExplicit,
				Trigger: webFallbackTriggerParallel,
			},
			overrides: map[string]string{
				settingKeyLegacyWebFallbackTrigger: "parallel",
			},
			want: webFallbackPrivacyExplicit,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &webFallbackMigrationStore{}
			if err := initializeWebFallbackOverride(
				t.Context(),
				store,
				nodeConfig{WebFallback: test.env},
				test.overrides,
			); err != nil {
				t.Fatal(err)
			}
			config := applyRuntimeSettingOverrides(
				nodeConfig{WebFallback: test.env},
				test.overrides,
			)
			if got := effectiveWebFallbackPrivacy(config.WebFallback); got != test.want {
				t.Fatalf("mode = %q, want %q; overrides = %v", got, test.want, test.overrides)
			}
			if store.calls != 1 {
				t.Fatalf("migration writes = %d, want one", store.calls)
			}
		})
	}
}

func TestVersionedWebFallbackSettingIsAuthoritative(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		env  webFallbackPrivacy
		want webFallbackPrivacy
	}{
		{
			"mode ignores legacy trigger",
			encodeWebFallbackSetting(webFallbackPrivacyEnabled),
			webFallbackPrivacyDisabled,
			webFallbackPrivacyEnabled,
		},
		{
			"environment ignores legacy trigger",
			webFallbackSettingEnvironment,
			webFallbackPrivacyDisabled,
			webFallbackPrivacyDisabled,
		},
		{
			"invalid version overrides always environment closed",
			webFallbackSettingPrefix + "invalid",
			webFallbackPrivacyAlways,
			webFallbackPrivacyDisabled,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := nodeConfig{WebFallback: webFallbackConfig{
				Privacy: test.env,
				Trigger: webFallbackTriggerMiss,
			}}
			overrides := map[string]string{
				settingKeyWebFallbackPrivacy:       test.raw,
				settingKeyLegacyWebFallbackTrigger: string(webFallbackTriggerParallel),
			}
			store := &webFallbackMigrationStore{}
			if err := initializeWebFallbackOverride(
				t.Context(),
				store,
				env,
				overrides,
			); err != nil {
				t.Fatal(err)
			}
			if store.calls != 0 {
				t.Fatalf("versioned setting was rewritten %d times", store.calls)
			}
			config := applyRuntimeSettingOverrides(env, overrides)
			if got := effectiveWebFallbackPrivacy(config.WebFallback); got != test.want {
				t.Fatalf("mode = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWebFallbackOverrideInitializationCreatesEnvironmentSlot(t *testing.T) {
	store := &webFallbackMigrationStore{}
	overrides := map[string]string{}
	if err := initializeWebFallbackOverride(
		t.Context(),
		store,
		nodeConfig{},
		overrides,
	); err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 ||
		overrides[settingKeyWebFallbackPrivacy] != webFallbackSettingEnvironment {
		t.Fatalf("initialization = %d/%v", store.calls, overrides)
	}
}

func TestLegacyWebFallbackOverrideMigrationNormalizesInvalidValues(t *testing.T) {
	overrides := map[string]string{
		settingKeyWebFallbackPrivacy:       "invalid",
		settingKeyLegacyWebFallbackTrigger: "invalid",
	}
	store := &webFallbackMigrationStore{}
	if err := initializeWebFallbackOverride(
		t.Context(),
		store,
		nodeConfig{WebFallback: webFallbackConfig{
			Privacy: webFallbackPrivacyEnabled,
			Trigger: webFallbackTriggerParallel,
		}},
		overrides,
	); err != nil {
		t.Fatal(err)
	}
	if overrides[settingKeyWebFallbackPrivacy] !=
		encodeWebFallbackSetting(webFallbackPrivacyEnabled) {
		t.Fatalf("overrides = %v", overrides)
	}
}

func TestLegacyWebFallbackOverrideMigrationReturnsWriteError(t *testing.T) {
	sentinel := errors.New("write failed")
	store := &webFallbackMigrationStore{err: sentinel}
	err := initializeWebFallbackOverride(
		t.Context(),
		store,
		nodeConfig{},
		map[string]string{settingKeyWebFallbackPrivacy: "enabled"},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}

func TestWebFallbackOverrideInitializationReturnsSlotWriteError(t *testing.T) {
	sentinel := errors.New("write failed")
	store := &webFallbackMigrationStore{err: sentinel}
	err := initializeWebFallbackOverride(
		t.Context(),
		store,
		nodeConfig{},
		map[string]string{},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeWebFallbackSettingLegacyValue(t *testing.T) {
	mode, authoritative, environment := decodeWebFallbackSetting("enabled")
	if mode != "" || authoritative || environment {
		t.Fatalf("decoded = %q/%v/%v", mode, authoritative, environment)
	}
}
