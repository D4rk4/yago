// Package settingsstore is a durable, admin-writable key/value store for the
// node's operator-overridable runtime settings. Each setting name maps to a
// string value; an absent name is the unset state that falls back to the
// environment-derived default. It lets operator toggles survive restarts and
// layer over the boot configuration. Secrets are never placed here.
package settingsstore

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const settingsBucket vault.Name = "runtime_settings"

type stringCodec struct{}

func (stringCodec) Encode(value string) ([]byte, error) {
	return []byte(value), nil
}

func (stringCodec) Decode(raw []byte) (string, error) {
	return string(raw), nil
}

// Store persists operator-overridable runtime settings in the node vault.
type Store struct {
	vault  *vault.Vault
	values *vault.Collection[string]
}

// Open registers the runtime-settings bucket on the shared vault.
func Open(v *vault.Vault) (*Store, error) {
	values, err := vault.Register(v, settingsBucket, stringCodec{})
	if err != nil {
		return nil, fmt.Errorf("register runtime settings: %w", err)
	}

	return &Store{vault: v, values: values}, nil
}

// Get returns the stored value for name and whether an override is present.
func (s *Store) Get(ctx context.Context, name string) (string, bool, error) {
	var (
		value string
		set   bool
	)

	err := s.vault.View(ctx, func(tx *vault.Txn) error {
		stored, found, err := s.values.Get(tx, vault.Key(name))
		if err != nil {
			return fmt.Errorf("read runtime setting %q: %w", name, err)
		}
		value, set = stored, found

		return nil
	})
	if err != nil {
		return "", false, fmt.Errorf("get runtime setting: %w", err)
	}

	return value, set, nil
}

// Set stores value as the override for name.
func (s *Store) Set(ctx context.Context, name, value string) error {
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := s.values.Put(tx, vault.Key(name), value); err != nil {
			return fmt.Errorf("write runtime setting %q: %w", name, err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("set runtime setting: %w", err)
	}

	return nil
}

// Unset removes any override for name, reverting it to the environment default.
func (s *Store) Unset(ctx context.Context, name string) error {
	if err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := s.values.Delete(tx, vault.Key(name)); err != nil {
			return fmt.Errorf("clear runtime setting %q: %w", name, err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("unset runtime setting: %w", err)
	}

	return nil
}

// All returns every stored override keyed by setting name.
func (s *Store) All(ctx context.Context) (map[string]string, error) {
	overrides := make(map[string]string)

	err := s.vault.View(ctx, func(tx *vault.Txn) error {
		if err := s.values.Scan(tx, nil, func(key vault.Key, value string) (bool, error) {
			overrides[string(key)] = value

			return true, nil
		}); err != nil {
			return fmt.Errorf("scan runtime settings: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list runtime settings: %w", err)
	}

	return overrides, nil
}
