package yagonode

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

func nodeIdentity(config nodeConfig) nodeidentity.Identity {
	return nodeidentity.Identity{
		Hash:        config.Hash,
		NetworkName: config.NetworkName,
		Name:        config.Name,
		Host:        config.AdvertiseHost,
		Port:        config.AdvertisePort,
		Flags:       config.Flags,
		Version:     version,
		Start:       time.Now(),
	}
}

func nodeIdentityWithBirthDate(
	ctx context.Context,
	config nodeConfig,
	vault *vault.Vault,
) (nodeidentity.Identity, error) {
	identity := nodeIdentity(config)
	birth, err := openRuntimePeerBirthDate(ctx, vault, time.Now, config.DeclaredBirthDate)
	if err != nil {
		return nodeidentity.Identity{}, fmt.Errorf("open peer birth date: %w", err)
	}
	identity.BirthDate = birth

	return identity, nil
}
