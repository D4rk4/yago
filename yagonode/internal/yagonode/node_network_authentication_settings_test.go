package yagonode

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagoproto"
)

func TestNetworkAuthenticationSettingsArePersistedAndSecretIsRedacted(t *testing.T) {
	environment := nodeConfig{
		NetworkAuthenticationMode:   yagoproto.NetworkAuthenticationUncontrolled,
		NetworkAuthenticationSecret: "environment-secret",
	}
	source, store, recorder := newTestSettingsSource(t, environment)
	item := settingViewItem(
		t,
		source.Settings(context.Background()),
		settingKeyNetworkAuthenticationSecret,
	)
	if item.Value != "" || !item.Sensitive || !item.Configured || item.Overridden {
		t.Fatalf("secret item = %+v", item)
	}

	result, err := source.Update(context.Background(), adminui.SettingsChange{
		Key: settingKeyNetworkAuthenticationSecret, Value: "stored-secret",
	})
	if err != nil || !result.OK || !result.RestartRequired {
		t.Fatalf("secret update = %+v, %v", result, err)
	}
	stored, set, err := store.Get(context.Background(), settingKeyNetworkAuthenticationSecret)
	if err != nil || !set || stored != "stored-secret" {
		t.Fatalf("stored secret = %q, %v, %v", stored, set, err)
	}
	event := recorder.Recent(1)
	if len(event) != 1 || strings.Contains(event[0].Message, "stored-secret") {
		t.Fatalf("secret event = %+v", event)
	}

	item = settingViewItem(
		t,
		source.Settings(context.Background()),
		settingKeyNetworkAuthenticationSecret,
	)
	if item.Value != "" || !item.Sensitive || !item.Configured || !item.Overridden {
		t.Fatalf("stored secret item = %+v", item)
	}
}

func TestNetworkAuthenticationModeRequiresDesiredSecret(t *testing.T) {
	source, store, _ := newTestSettingsSource(t, nodeConfig{
		NetworkAuthenticationMode: yagoproto.NetworkAuthenticationUncontrolled,
	})
	ctx := context.Background()
	result, err := source.Update(ctx, adminui.SettingsChange{
		Key:   settingKeyNetworkAuthenticationMode,
		Value: string(yagoproto.NetworkAuthenticationSaltedMagic),
	})
	if err != nil || result.OK || !strings.Contains(result.Message, "requires a shared secret") {
		t.Fatalf("mode without secret = %+v, %v", result, err)
	}
	if _, set, err := store.Get(ctx, settingKeyNetworkAuthenticationMode); err != nil || set {
		t.Fatalf("invalid mode persisted: set=%v err=%v", set, err)
	}

	if result, err = source.Update(ctx, adminui.SettingsChange{
		Key: settingKeyNetworkAuthenticationSecret, Value: "shared",
	}); err != nil || !result.OK {
		t.Fatalf("secret update = %+v, %v", result, err)
	}
	if result, err = source.Update(ctx, adminui.SettingsChange{
		Key:   settingKeyNetworkAuthenticationMode,
		Value: string(yagoproto.NetworkAuthenticationSaltedMagic),
	}); err != nil || !result.OK {
		t.Fatalf("controlled mode update = %+v, %v", result, err)
	}
	result, err = source.Update(ctx, adminui.SettingsChange{
		Key: settingKeyNetworkAuthenticationSecret, Reset: true,
	})
	if err != nil || result.OK {
		t.Fatalf("unsafe secret reset = %+v, %v", result, err)
	}
}

func TestNetworkAuthenticationSettingDefinitionsRoundTrip(t *testing.T) {
	definitions := indexSettingDefinitions()
	secret := definitions[settingKeyNetworkAuthenticationSecret]
	mode := definitions[settingKeyNetworkAuthenticationMode]
	if !secret.sensitive || !secret.restartRequired() || !mode.restartRequired() {
		t.Fatalf(
			"definitions = secret sensitive=%v secret restart=%v mode restart=%v",
			secret.sensitive,
			secret.restartRequired(),
			mode.restartRequired(),
		)
	}
	config := secret.apply(nodeConfig{}, "shared")
	config = mode.apply(config, string(yagoproto.NetworkAuthenticationSaltedMagic))
	if !validNetworkAuthentication(config) {
		t.Fatalf("applied config = %+v", config)
	}
	for _, invalid := range []string{"", strings.Repeat("x", 1025), "line\nbreak"} {
		if _, err := secret.normalize(invalid); err == nil {
			t.Fatalf("secret %q accepted", invalid)
		}
	}
	if _, err := mode.normalize("unknown"); err == nil {
		t.Fatal("unknown mode accepted")
	}
}
