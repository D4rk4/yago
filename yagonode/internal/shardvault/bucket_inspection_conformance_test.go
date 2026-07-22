package shardvault

import (
	"path/filepath"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagonode/internal/vaulttest"
)

func TestBucketInspectionConformance(t *testing.T) {
	vaulttest.RunBucketInspectionConformance(
		t,
		func(quotaBytes int64) (*vault.Vault, error) {
			return Open(filepath.Join(t.TempDir(), "vault"), quotaBytes)
		},
	)
}
