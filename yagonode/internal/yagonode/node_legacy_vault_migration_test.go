package yagonode

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestLegacyVaultMigrationPreflight(t *testing.T) {
	directory := t.TempDir()
	missing := filepath.Join(directory, "missing.db")
	if err := preflightLegacyVaultMigration(
		missing,
		directory,
		yagocrawlcontract.StoragePressurePolicy{},
	); err != nil {
		t.Fatalf("missing legacy vault: %v", err)
	}
	legacy := filepath.Join(directory, "legacy.db")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o600); err != nil {
		t.Fatalf("write legacy vault fixture: %v", err)
	}
	if err := preflightLegacyVaultMigration(
		legacy,
		directory,
		yagocrawlcontract.StoragePressurePolicy{},
	); err != nil {
		t.Fatalf("available migration headroom: %v", err)
	}
	err := preflightLegacyVaultMigration(
		legacy,
		directory,
		yagocrawlcontract.StoragePressurePolicy{ReservedFreeBytes: math.MaxUint64},
	)
	if (!errors.Is(err, yagocrawlcontract.ErrStorageHeadroom) &&
		!errors.Is(err, yagocrawlcontract.ErrStoragePressure)) ||
		!strings.Contains(err.Error(), envStorageReservedFree) {
		t.Fatalf("insufficient migration headroom error = %v", err)
	}
	directoryPath := filepath.Join(directory, "legacy-directory")
	if err := os.Mkdir(directoryPath, 0o750); err != nil {
		t.Fatalf("create legacy directory: %v", err)
	}
	if err := preflightLegacyVaultMigration(
		directoryPath,
		directory,
		yagocrawlcontract.StoragePressurePolicy{},
	); err != nil {
		t.Fatalf("non-file legacy path: %v", err)
	}
	loop := filepath.Join(directory, "legacy-loop")
	if err := os.Symlink("legacy-loop", loop); err != nil {
		t.Fatalf("create legacy symlink loop: %v", err)
	}
	if err := preflightLegacyVaultMigration(
		loop,
		directory,
		yagocrawlcontract.StoragePressurePolicy{},
	); err == nil || !strings.Contains(err.Error(), "inspect legacy vault") {
		t.Fatalf("legacy inspection error = %v", err)
	}
}
