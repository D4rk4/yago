package urlmeta

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

type recordingObserver struct {
	stored []yacymodel.Hash
	purged []yacymodel.Hash
	fail   bool
}

func (r *recordingObserver) URLStored(_ *boltvault.Txn, hash yacymodel.Hash, _ string) error {
	r.stored = append(r.stored, hash)
	if r.fail {
		return fmt.Errorf("observer refused store")
	}

	return nil
}

func (r *recordingObserver) URLPurged(_ *boltvault.Txn, hash yacymodel.Hash) error {
	r.purged = append(r.purged, hash)
	if r.fail {
		return fmt.Errorf("observer refused purge")
	}

	return nil
}

func openObservedModule(
	t *testing.T,
	watchers ...URLMetadataObserver,
) (*boltvault.Vault, urlPorts) {
	t.Helper()

	vault, err := boltvault.Open(filepath.Join(t.TempDir(), "node.db"), 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := vault.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	directory, evictor, receiver, err := Open(vault, watchers...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return vault, urlPorts{Directory: directory, Evictor: evictor, Receiver: receiver}
}
