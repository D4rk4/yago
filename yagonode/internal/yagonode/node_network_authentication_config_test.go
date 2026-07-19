package yagonode

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagoproto"
)

func TestNetworkAuthenticationDefaultsToUncontrolled(t *testing.T) {
	config, err := loadNetworkAuthentication(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if config.mode != yagoproto.NetworkAuthenticationUncontrolled || config.secret != "" {
		t.Fatalf("config = %+v", config)
	}
}

func TestUncontrolledNetworkPreservesStagedSecret(t *testing.T) {
	config, err := loadNetworkAuthentication(environmentValues{
		envNetworkAuthenticationMaterial: "staged",
	}.get)
	if err != nil {
		t.Fatal(err)
	}
	if config.mode != yagoproto.NetworkAuthenticationUncontrolled || config.secret != "staged" {
		t.Fatalf("config = %+v", config)
	}
}

func TestNetworkAuthenticationLoadsSaltedMagic(t *testing.T) {
	values := environmentValues{
		envNetworkAuthentication:         string(yagoproto.NetworkAuthenticationSaltedMagic),
		envNetworkAuthenticationMaterial: "shared secret",
	}
	config, err := loadNetworkAuthentication(values.get)
	if err != nil {
		t.Fatal(err)
	}
	if config.mode != yagoproto.NetworkAuthenticationSaltedMagic ||
		config.secret != "shared secret" {
		t.Fatalf("config = %+v", config)
	}
}

func TestNetworkAuthenticationRejectsMissingSecretAndUnknownMode(t *testing.T) {
	for _, values := range []environmentValues{
		{envNetworkAuthentication: string(yagoproto.NetworkAuthenticationSaltedMagic)},
		{envNetworkAuthentication: "unknown"},
	} {
		_, err := loadNetworkAuthentication(values.get)
		if err == nil || !strings.Contains(err.Error(), envNetworkAuthentication) {
			t.Fatalf("error = %v", err)
		}
	}
	if _, err := loadNodeConfig(environmentValues{
		envNetworkAuthentication: "unknown",
	}.get); err == nil {
		t.Fatal("node configuration accepted an unknown authentication mode")
	}
}

type environmentValues map[string]string

func (v environmentValues) get(key string) string {
	return v[key]
}
