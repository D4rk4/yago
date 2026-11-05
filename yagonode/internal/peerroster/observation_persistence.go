package peerroster

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (r *roster) persistObservation(
	ctx context.Context,
	observation rosterEntry,
	insertOnly bool,
	answered yagomodel.Optional[peerwire.Endpoint],
) (bool, error) {
	admission := r.endpointAdmission(observation)
	if !admission.accepted {
		return false, nil
	}
	stored := false
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		key := r.key(observation.seed.Hash)
		if insertOnly && r.peers.Contains(tx, key) {
			return nil
		}
		if err := r.putRosterEntry(tx, key, observation); err != nil {
			return fmt.Errorf("store peer observation: %w", err)
		}
		if err := r.storeAnsweredEndpoint(tx, key, observation, answered); err != nil {
			return err
		}
		stored = true

		return nil
	}); err != nil {
		return false, fmt.Errorf("persist peer observation: %w", err)
	}
	if !stored {
		return false, nil
	}
	r.mu.Lock()
	for _, displaced := range admission.displaced {
		delete(r.active, displaced)
	}
	r.mu.Unlock()
	r.applyEndpointAdmission(observation, admission)

	return true, nil
}
