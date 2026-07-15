// Package rankingprofile persists the node's default search ranking weights and
// serves them live to the local searcher, so an operator can retune ranking
// without a restart and the setting survives one. The weights themselves and
// their validation live in the searchindex package; this package only stores the
// operator-chosen default and hands it out atomically.
package rankingprofile

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const profileBucket vault.Name = "rankingprofile"

var profileKey = vault.Key("default")

type weightsCodec struct{}

func (weightsCodec) Encode(weights searchindex.RankingWeights) ([]byte, error) {
	raw, err := json.Marshal(weights)
	if err != nil {
		return nil, fmt.Errorf("encode ranking weights: %w", err)
	}

	return raw, nil
}

func (weightsCodec) Decode(raw []byte) (searchindex.RankingWeights, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return searchindex.RankingWeights{}, fmt.Errorf("decode ranking weights: %w", err)
	}
	var weights searchindex.RankingWeights
	if err := json.Unmarshal(raw, &weights); err != nil {
		return searchindex.RankingWeights{}, fmt.Errorf("decode ranking weights: %w", err)
	}
	for _, definition := range searchindex.RankingWeightDefinitions() {
		if _, present := fields[definition.Key]; !present && definition.BackfillWhenMissing {
			weights.Set(definition.Key, definition.Default)
		}
	}
	if err := weights.ValidatePersisted(); err != nil {
		return searchindex.RankingWeights{}, fmt.Errorf("decode ranking weights: %w", err)
	}

	return weights, nil
}

// Holder serves the current default ranking weights atomically and persists
// changes to the vault. Current is nil-safe so the holder can double as the
// searcher's weights provider.
type Holder struct {
	vault   *vault.Vault
	weights *vault.Collection[searchindex.RankingWeights]
	current atomic.Pointer[searchindex.RankingWeights]
}

// Open registers the ranking-profile bucket and loads the persisted default,
// falling back to the built-in default when none is stored.
func Open(ctx context.Context, v *vault.Vault) (*Holder, error) {
	weights, err := vault.Register(v, profileBucket, weightsCodec{})
	if err != nil {
		return nil, fmt.Errorf("register ranking profile: %w", err)
	}
	holder := &Holder{vault: v, weights: weights}

	stored := searchindex.DefaultRankingWeights()
	if err := v.View(ctx, func(tx *vault.Txn) error {
		value, found, err := weights.Get(tx, profileKey)
		if err != nil {
			return fmt.Errorf("read ranking profile: %w", err)
		}
		if found {
			stored = value
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("load ranking profile: %w", err)
	}
	holder.current.Store(&stored)

	return holder, nil
}

// Current returns the live default ranking weights, or the built-in default when
// the holder is nil or unset.
func (h *Holder) Current() searchindex.RankingWeights {
	if h == nil {
		return searchindex.DefaultRankingWeights()
	}
	if weights := h.current.Load(); weights != nil {
		return *weights
	}

	return searchindex.DefaultRankingWeights()
}

// Set validates, persists, and atomically applies new default ranking weights.
func (h *Holder) Set(ctx context.Context, weights searchindex.RankingWeights) error {
	if err := weights.Validate(); err != nil {
		return fmt.Errorf("invalid ranking weights: %w", err)
	}
	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := h.weights.Put(tx, profileKey, weights); err != nil {
			return fmt.Errorf("write ranking profile: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("persist ranking profile: %w", err)
	}
	h.current.Store(&weights)

	return nil
}
