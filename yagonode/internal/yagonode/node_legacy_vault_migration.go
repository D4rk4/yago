package yagonode

import (
	"errors"
	"fmt"
	"os"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func preflightLegacyVaultMigration(
	legacyPath string,
	dataDirectory string,
	policy yagocrawlcontract.StoragePressurePolicy,
) error {
	info, err := os.Stat(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect legacy vault: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	required := uint64(max(info.Size(), 0))
	gate := yagocrawlcontract.NewStoragePressureGate(dataDirectory, policy)
	if err := gate.CheckGrowthWithHeadroom(required); err != nil {
		return fmt.Errorf(
			"legacy vault migration needs %s above the reserved free storage; free filesystem space or lower %s: %w",
			humanUnsignedBytes(required),
			envStorageReservedFree,
			err,
		)
	}

	return nil
}
