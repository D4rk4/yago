// Package seedimport keeps a durable per-URL record of the last seed-list import
// so the admin console can show when each configured seed list was last refreshed
// and whether it succeeded. It is a small vault-backed status store, updated by the
// operator's on-demand refresh action.
package seedimport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const statusBucket vault.Name = "seedimport-status"

// Status is the outcome of the most recent import of one seed-list URL.
type Status struct {
	LastImport time.Time `json:"lastImport"`
	Seeds      int       `json:"seeds"`
	OK         bool      `json:"ok"`
	Error      string    `json:"error,omitempty"`
}

type statusCodec struct{}

func (statusCodec) Encode(status Status) ([]byte, error) {
	data, _ := json.Marshal(status)

	return data, nil
}

func (statusCodec) Decode(raw []byte) (Status, error) {
	var status Status
	if err := json.Unmarshal(raw, &status); err != nil {
		return Status{}, fmt.Errorf("decode seed import status: %w", err)
	}

	return status, nil
}

// Store persists the last-import status per seed-list URL.
type Store struct {
	vault  *vault.Vault
	status *vault.Collection[Status]
	now    func() time.Time
}

// Open registers the status collection on the shared vault.
func Open(v *vault.Vault, now func() time.Time) (*Store, error) {
	status, err := vault.Register(v, statusBucket, statusCodec{})
	if err != nil {
		return nil, fmt.Errorf("register seed import status: %w", err)
	}

	return &Store{vault: v, status: status, now: now}, nil
}

// Record upserts the outcome of importing one URL. A non-nil err records a failure
// with its message; a nil err records success with the seed count.
func (s *Store) Record(ctx context.Context, url string, seeds int, importErr error) error {
	status := Status{LastImport: s.now(), Seeds: seeds, OK: importErr == nil}
	if importErr != nil {
		status.Error = importErr.Error()
	}

	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := s.status.Put(tx, vault.Key(url), status); err != nil {
			return fmt.Errorf("store seed import status: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("update seed import status: %w", err)
	}

	return nil
}

// Get returns the stored status for a URL, or false when it has never been
// imported through this store.
func (s *Store) Get(ctx context.Context, url string) (Status, bool, error) {
	var (
		status Status
		found  bool
	)
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		got, ok, err := s.status.Get(tx, vault.Key(url))
		if err != nil {
			return fmt.Errorf("read seed import status: %w", err)
		}
		status, found = got, ok

		return nil
	}); err != nil {
		return Status{}, false, fmt.Errorf("view seed import status: %w", err)
	}

	return status, found, nil
}
