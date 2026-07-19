package yagonode

import (
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagoproto"
)

const maximumNetworkAuthenticationSecretBytes = 1024

func validateConfiguredNetworkAuthenticationSecret(secret string) error {
	if secret == "" || len(secret) > maximumNetworkAuthenticationSecretBytes ||
		strings.ContainsAny(secret, "\x00\r\n") {
		return fmt.Errorf(
			"shared secret must contain 1 to %d bytes on one line",
			maximumNetworkAuthenticationSecretBytes,
		)
	}

	return nil
}

func validateNetworkAuthenticationSecret(
	mode yagoproto.NetworkAuthenticationMode,
	secret string,
) error {
	switch mode {
	case yagoproto.NetworkAuthenticationUncontrolled:
		if secret == "" {
			return nil
		}
	case yagoproto.NetworkAuthenticationSaltedMagic:
		if secret == "" {
			return fmt.Errorf("shared secret is required")
		}
	default:
		return fmt.Errorf("unsupported network authentication mode")
	}

	return validateConfiguredNetworkAuthenticationSecret(secret)
}
