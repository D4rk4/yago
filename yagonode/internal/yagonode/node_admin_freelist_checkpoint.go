package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type freelistCheckpointSource struct {
	store *vault.Vault
}

func newFreelistCheckpointSource(store *vault.Vault) adminui.FreelistCheckpointer {
	if store == nil || !store.SupportsFreelistCheckpoint() {
		return nil
	}

	return freelistCheckpointSource{store: store}
}

func (s freelistCheckpointSource) CheckpointFreelists(
	ctx context.Context,
) (adminui.FreelistCheckpointResult, error) {
	result, err := s.store.CheckpointFreelists(ctx)
	if err != nil {
		return adminui.FreelistCheckpointResult{
			ShardsCheckpointed: result.ShardsCheckpointed,
			ShardsTotal:        result.ShardsTotal,
		}, fmt.Errorf("save vault freelists: %w", err)
	}

	return adminui.FreelistCheckpointResult{
		ShardsCheckpointed: result.ShardsCheckpointed,
		ShardsTotal:        result.ShardsTotal,
	}, nil
}
