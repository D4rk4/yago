// Package eventstore is a durable, bounded log of structured node events so the
// admin event log survives restarts. It keeps the most recent events in the node
// vault, keyed by a monotonic sequence, and prunes the oldest beyond a cap. It
// stores no secrets — only the event severity, category, name and message.
package eventstore

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const eventsBucket vault.Name = "events"

const (
	defaultCapacity = 512
	sequenceWidth   = 8
)

type eventCodec struct{}

func (eventCodec) Encode(event events.Event) ([]byte, error) {
	raw, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("encode event: %w", err)
	}

	return raw, nil
}

func (eventCodec) Decode(raw []byte) (events.Event, error) {
	var event events.Event
	if err := json.Unmarshal(raw, &event); err != nil {
		return events.Event{}, fmt.Errorf("decode event: %w", err)
	}

	return event, nil
}

// Store is a durable bounded event log backed by the node vault.
type Store struct {
	vault    *vault.Vault
	events   *vault.Collection[events.Event]
	capacity uint64
	mu       sync.Mutex
	next     uint64
}

// Open registers the events bucket on the shared vault and resumes the sequence
// from any events that survived the previous run.
func Open(ctx context.Context, v *vault.Vault) (*Store, error) {
	return OpenWithCapacity(ctx, v, defaultCapacity)
}

// OpenWithCapacity is Open with an explicit retention cap (events kept on disk).
func OpenWithCapacity(ctx context.Context, v *vault.Vault, capacity int) (*Store, error) {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	collection, err := vault.Register(v, eventsBucket, eventCodec{})
	if err != nil {
		return nil, fmt.Errorf("register events: %w", err)
	}
	store := &Store{vault: v, events: collection, capacity: uint64(capacity)}
	if err := store.resume(ctx); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *Store) resume(ctx context.Context) error {
	err := s.vault.View(ctx, func(tx *vault.Txn) error {
		return s.events.Scan(tx, nil, func(key vault.Key, _ events.Event) (bool, error) {
			if len(key) == sequenceWidth {
				if seq := binary.BigEndian.Uint64(key); seq+1 > s.next {
					s.next = seq + 1
				}
			}

			return true, nil
		})
	})
	if err != nil {
		return fmt.Errorf("resume event sequence: %w", err)
	}

	return nil
}

// Append stores one event and prunes the oldest once the cap is exceeded.
func (s *Store) Append(ctx context.Context, event events.Event) error {
	s.mu.Lock()
	seq := s.next
	s.next++
	s.mu.Unlock()

	err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := s.events.Put(tx, sequenceKey(seq), event); err != nil {
			return fmt.Errorf("append event: %w", err)
		}
		if seq >= s.capacity {
			if _, err := s.events.Delete(tx, sequenceKey(seq-s.capacity)); err != nil {
				return fmt.Errorf("prune event: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("persist event: %w", err)
	}

	return nil
}

// Recent returns the stored events oldest-first (ascending sequence), suitable
// for seeding the in-memory recorder ring.
func (s *Store) Recent(ctx context.Context) ([]events.Event, error) {
	var stored []events.Event

	err := s.vault.View(ctx, func(tx *vault.Txn) error {
		return s.events.Scan(tx, nil, func(_ vault.Key, event events.Event) (bool, error) {
			stored = append(stored, event)

			return true, nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}

	return stored, nil
}

func sequenceKey(seq uint64) vault.Key {
	key := make([]byte, sequenceWidth)
	binary.BigEndian.PutUint64(key, seq)

	return vault.Key(key)
}
