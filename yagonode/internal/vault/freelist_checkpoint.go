package vault

import (
	"context"
	"errors"
	"fmt"
)

var ErrFreelistCheckpointUnsupported = errors.New("freelist checkpoint unsupported")

type FreelistCheckpointResult struct {
	ShardsCheckpointed int
	ShardsTotal        int
}

type freelistCheckpointer interface {
	CheckpointFreelists(context.Context) (FreelistCheckpointResult, error)
}

func (v *Vault) SupportsFreelistCheckpoint() bool {
	lease, err := v.acquireEngineLease()
	if err != nil {
		return false
	}
	defer lease.release()
	_, ok := lease.engine.(freelistCheckpointer)

	return ok
}

func (v *Vault) CheckpointFreelists(ctx context.Context) (FreelistCheckpointResult, error) {
	lease, err := v.acquireEngineLease()
	if err != nil {
		return FreelistCheckpointResult{}, err
	}
	defer lease.release()
	checkpointer, ok := lease.engine.(freelistCheckpointer)
	if !ok {
		return FreelistCheckpointResult{}, ErrFreelistCheckpointUnsupported
	}
	result, err := checkpointer.CheckpointFreelists(ctx)
	if err != nil {
		return result, fmt.Errorf("checkpoint vault freelists: %w", err)
	}

	return result, nil
}
