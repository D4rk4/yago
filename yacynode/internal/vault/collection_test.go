package vault_test

import (
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

func TestNilEngineRejected(t *testing.T) {
	if _, err := vault.New(nil); err == nil {
		t.Fatal("New(nil) succeeded, want error")
	}
}
