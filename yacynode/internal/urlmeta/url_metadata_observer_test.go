package urlmeta

import (
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/memvault"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type recordingObserver struct {
	stored []yacymodel.Hash
	purged []yacymodel.Hash
	fail   bool
}

func (r *recordingObserver) URLStored(_ *vault.Txn, hash yacymodel.Hash, _ string) error {
	r.stored = append(r.stored, hash)
	if r.fail {
		return fmt.Errorf("observer refused store")
	}

	return nil
}

func (r *recordingObserver) URLPurged(_ *vault.Txn, hash yacymodel.Hash) error {
	r.purged = append(r.purged, hash)
	if r.fail {
		return fmt.Errorf("observer refused purge")
	}

	return nil
}

func openObservedModule(
	t *testing.T,
	watchers ...URLMetadataObserver,
) (*vault.Vault, urlPorts) {
	t.Helper()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	directory, evictor, receiver, err := Open(v, watchers...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return v, urlPorts{Directory: directory, Evictor: evictor, Receiver: receiver}
}
