// Package peerblock keeps a durable operator-managed blocklist of peer hashes.
// Blocked peers are excluded from outbound index fan-out and from the peer lists
// this node advertises to others. The list survives restarts as a small
// vault-backed collection keyed by peer hash.
package peerblock

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const blockBucket vault.Name = "peerblock"

type record struct {
	BlockedAt time.Time `json:"blockedAt"`
}

type recordCodec struct{}

func (recordCodec) Encode(rec record) ([]byte, error) {
	data, _ := json.Marshal(rec)

	return data, nil
}

func (recordCodec) Decode(raw []byte) (record, error) {
	var rec record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return record{}, fmt.Errorf("decode peer block record: %w", err)
	}

	return rec, nil
}

// Blocked is one blocked peer with the time it was blocked.
type Blocked struct {
	Hash      yagomodel.Hash
	BlockedAt time.Time
}

// Store persists the operator's peer blocklist.
type Store struct {
	vault   *vault.Vault
	records *vault.Collection[record]
	now     func() time.Time
}

// Open registers the blocklist collection on the shared vault.
func Open(v *vault.Vault, now func() time.Time) (*Store, error) {
	records, err := vault.Register(v, blockBucket, recordCodec{})
	if err != nil {
		return nil, fmt.Errorf("register peer block: %w", err)
	}

	return &Store{vault: v, records: records, now: now}, nil
}

func (s *Store) key(hash yagomodel.Hash) vault.Key {
	return vault.Key(hash.String())
}

// Block adds a peer to the blocklist. Blocking an already-blocked peer refreshes
// its recorded time and is not an error.
func (s *Store) Block(ctx context.Context, hash yagomodel.Hash) error {
	rec := record{BlockedAt: s.now().UTC()}
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := s.records.Put(tx, s.key(hash), rec); err != nil {
			return fmt.Errorf("store peer block: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("update peer block: %w", err)
	}

	return nil
}

// Unblock removes a peer from the blocklist. Unblocking a peer that is not blocked
// is not an error.
func (s *Store) Unblock(ctx context.Context, hash yagomodel.Hash) error {
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := s.records.Delete(tx, s.key(hash)); err != nil {
			return fmt.Errorf("delete peer block: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("update peer block: %w", err)
	}

	return nil
}

// IsBlocked reports whether a peer is on the blocklist.
func (s *Store) IsBlocked(ctx context.Context, hash yagomodel.Hash) (bool, error) {
	var blocked bool
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		_, ok, err := s.records.Get(tx, s.key(hash))
		if err != nil {
			return fmt.Errorf("read peer block: %w", err)
		}
		blocked = ok

		return nil
	}); err != nil {
		return false, fmt.Errorf("view peer block: %w", err)
	}

	return blocked, nil
}

// Blocked returns every blocked peer with the time it was blocked.
func (s *Store) Blocked(ctx context.Context) ([]Blocked, error) {
	var blocked []Blocked
	if err := s.vault.View(ctx, func(tx *vault.Txn) error {
		return s.records.Scan(tx, nil, func(key vault.Key, rec record) (bool, error) {
			blocked = append(blocked, Blocked{
				Hash:      yagomodel.Hash(string(key)),
				BlockedAt: rec.BlockedAt,
			})

			return true, nil
		})
	}); err != nil {
		return nil, fmt.Errorf("view peer block: %w", err)
	}

	return blocked, nil
}
