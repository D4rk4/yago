package yagonode

import (
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagoproto"
)

const (
	envNetworkAuthentication         = "YAGO_NETWORK_AUTHENTICATION"
	envNetworkAuthenticationMaterial = "YAGO_NETWORK_AUTHENTICATION_SECRET"
)

type networkAuthenticationConfig struct {
	mode   yagoproto.NetworkAuthenticationMode
	secret string
}

func loadNetworkAuthentication(getenv func(string) string) (networkAuthenticationConfig, error) {
	mode := yagoproto.NetworkAuthenticationMode(strings.TrimSpace(getenv(envNetworkAuthentication)))
	if mode == "" {
		mode = yagoproto.NetworkAuthenticationUncontrolled
	}
	secret := getenv(envNetworkAuthenticationMaterial)
	config := networkAuthenticationConfig{mode: mode, secret: secret}
	if mode != yagoproto.NetworkAuthenticationUncontrolled &&
		mode != yagoproto.NetworkAuthenticationSaltedMagic {
		return networkAuthenticationConfig{}, fmt.Errorf(
			"%s must be %s or %s",
			envNetworkAuthentication,
			yagoproto.NetworkAuthenticationUncontrolled,
			yagoproto.NetworkAuthenticationSaltedMagic,
		)
	}
	if err := validateNetworkAuthenticationSecret(mode, secret); err != nil {
		return networkAuthenticationConfig{}, fmt.Errorf(
			"%s: %w",
			envNetworkAuthenticationMaterial,
			err,
		)
	}

	return config, nil
}

func validNetworkAuthentication(config nodeConfig) bool {
	return validateNetworkAuthenticationSecret(
		config.NetworkAuthenticationMode,
		config.NetworkAuthenticationSecret,
	) == nil
}
