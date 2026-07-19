package yagonode

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/peeridentity"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// resolvePeerIdentity fills in the effective peer hash and name from the data
// directory, generating and persisting them when neither the environment nor a
// previous run supplied a value, so a node bootstraps without a mandatory
// identity yet keeps a stable one across restarts.
func resolvePeerIdentity(
	ctx context.Context,
	v *vault.Vault,
	config nodeConfig,
) (nodeConfig, error) {
	hash, name, err := peeridentity.Open(
		ctx, v, config.Hash, config.Name, peeridentity.DefaultGenerators(),
	)
	if err != nil {
		return config, fmt.Errorf("resolve peer identity: %w", err)
	}
	config.Hash = hash
	config.Name = name

	return config, nil
}

func nodeIdentity(config nodeConfig) nodeidentity.Identity {
	return nodeidentity.Identity{
		Hash:                     config.Hash,
		NetworkName:              config.NetworkName,
		Name:                     config.Name,
		Host:                     config.AdvertiseHost,
		Port:                     config.AdvertisePort,
		Flags:                    config.Flags,
		Version:                  version,
		Start:                    time.Now(),
		AuthenticationMode:       config.NetworkAuthenticationMode,
		AuthenticationEssentials: config.NetworkAuthenticationSecret,
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
