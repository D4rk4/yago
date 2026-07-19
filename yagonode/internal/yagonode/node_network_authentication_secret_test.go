package yagonode

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagoproto"
)

func TestNetworkAuthenticationSecretBootstrapAndAdminShareBounds(t *testing.T) {
	definition := indexSettingDefinitions()[settingKeyNetworkAuthenticationSecret]
	valid := []string{
		"x",
		strings.Repeat("x", maximumNetworkAuthenticationSecretBytes),
		strings.Repeat("é", maximumNetworkAuthenticationSecretBytes/2),
	}
	for _, secret := range valid {
		config, err := loadNetworkAuthentication(environmentValues{
			envNetworkAuthentication:         string(yagoproto.NetworkAuthenticationSaltedMagic),
			envNetworkAuthenticationMaterial: secret,
		}.get)
		if err != nil || config.secret != secret {
			t.Fatalf("bootstrap secret bytes=%d: %+v, %v", len(secret), config, err)
		}
		if normalized, err := definition.normalize(secret); err != nil || normalized != secret {
			t.Fatalf("Admin secret bytes=%d: %q, %v", len(secret), normalized, err)
		}
	}

	invalid := []string{
		strings.Repeat("x", maximumNetworkAuthenticationSecretBytes+1),
		"line\nbreak",
		"line\rbreak",
		"nul\x00byte",
	}
	for _, secret := range invalid {
		for _, mode := range []yagoproto.NetworkAuthenticationMode{
			yagoproto.NetworkAuthenticationUncontrolled,
			yagoproto.NetworkAuthenticationSaltedMagic,
		} {
			if _, err := loadNetworkAuthentication(environmentValues{
				envNetworkAuthentication:         string(mode),
				envNetworkAuthenticationMaterial: secret,
			}.get); err == nil {
				t.Fatalf("bootstrap accepted mode=%s secret bytes=%d", mode, len(secret))
			}
		}
		if _, err := definition.normalize(secret); err == nil {
			t.Fatalf("Admin accepted secret bytes=%d", len(secret))
		}
	}
}

func TestNetworkAuthenticationAllowsEmptySecretOnlyWhenUncontrolled(t *testing.T) {
	if _, err := loadNetworkAuthentication(environmentValues{
		envNetworkAuthentication: string(yagoproto.NetworkAuthenticationUncontrolled),
	}.get); err != nil {
		t.Fatalf("uncontrolled empty secret: %v", err)
	}
	if _, err := loadNetworkAuthentication(environmentValues{
		envNetworkAuthentication: string(yagoproto.NetworkAuthenticationSaltedMagic),
	}.get); err == nil {
		t.Fatal("salted-magic accepted an empty secret")
	}
	if validNetworkAuthentication(nodeConfig{
		NetworkAuthenticationMode:   yagoproto.NetworkAuthenticationUncontrolled,
		NetworkAuthenticationSecret: "line\nbreak",
	}) {
		t.Fatal("runtime accepted an invalid staged secret")
	}
}

func TestNetworkAuthenticationAdminRejectsInvalidSecretBeforePersistence(t *testing.T) {
	source, store, _ := newTestSettingsSource(t, nodeConfig{
		NetworkAuthenticationMode: yagoproto.NetworkAuthenticationUncontrolled,
	})
	result, err := source.Update(context.Background(), adminui.SettingsChange{
		Key:   settingKeyNetworkAuthenticationSecret,
		Value: strings.Repeat("x", maximumNetworkAuthenticationSecretBytes+1),
	})
	if err != nil || result.OK {
		t.Fatalf("invalid Admin update = %+v, %v", result, err)
	}
	if _, set, err := store.Get(
		context.Background(),
		settingKeyNetworkAuthenticationSecret,
	); err != nil ||
		set {
		t.Fatalf("invalid secret persisted: set=%t err=%v", set, err)
	}
}
