// Package crawlformats persists the operator's shared document-format toggles:
// which format families every crawl parses (YaCy TextParser parity). The node
// stamps them into each dispatched crawl profile, so one console setting
// governs all crawls.
package crawlformats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const formatsBucket = "crawl_formats"

// togglesKey addresses the single toggles record inside the bucket.
var togglesKey = vault.Key("toggles")

type togglesCodec struct{}

func (togglesCodec) Encode(toggles yagocrawlcontract.FormatToggles) ([]byte, error) {
	encoded, err := json.Marshal(toggles)
	if err != nil {
		return nil, fmt.Errorf("encode crawl formats: %w", err)
	}

	return encoded, nil
}

func (togglesCodec) Decode(raw []byte) (yagocrawlcontract.FormatToggles, error) {
	var toggles yagocrawlcontract.FormatToggles
	if err := json.Unmarshal(raw, &toggles); err != nil {
		return yagocrawlcontract.FormatToggles{}, fmt.Errorf("decode crawl formats: %w", err)
	}

	return toggles, nil
}

// Store reads and writes the shared format toggles in the vault.
type Store struct {
	vault  *vault.Vault
	values *vault.Collection[yagocrawlcontract.FormatToggles]
}

// Open registers the format-toggles bucket on the shared vault.
func Open(v *vault.Vault) (*Store, error) {
	values, err := vault.Register(v, formatsBucket, togglesCodec{})
	if err != nil {
		return nil, fmt.Errorf("register crawl formats: %w", err)
	}

	return &Store{vault: v, values: values}, nil
}

// Current returns the persisted toggles, or the defaults (everything on except
// archives) when none were saved or the record does not decode.
func (s *Store) Current(ctx context.Context) yagocrawlcontract.FormatToggles {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	_ = s.vault.View(ctx, func(tx *vault.Txn) error {
		if stored, ok, err := s.values.Get(tx, togglesKey); err == nil && ok {
			toggles = stored
		}

		return nil
	})

	return toggles
}

// Set persists new toggles.
func (s *Store) Set(ctx context.Context, toggles yagocrawlcontract.FormatToggles) error {
	err := s.vault.Update(ctx, func(tx *vault.Txn) error {
		return s.values.Put(tx, togglesKey, toggles)
	})
	if err != nil {
		return fmt.Errorf("persist crawl formats: %w", err)
	}

	return nil
}
