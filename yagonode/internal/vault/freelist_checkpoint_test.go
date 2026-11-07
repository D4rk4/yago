package vault_test

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type freelistCheckpointEngine struct {
	*doubleEngine
	result vault.FreelistCheckpointResult
	err    error
	calls  int
}

func (e *freelistCheckpointEngine) CheckpointFreelists(
	context.Context,
) (vault.FreelistCheckpointResult, error) {
	e.calls++

	return e.result, e.err
}

func TestVaultFreelistCheckpointCapability(t *testing.T) {
	ctx := context.Background()
	var nilVault *vault.Vault
	if nilVault.SupportsFreelistCheckpoint() {
		t.Fatal("nil vault supports a freelist checkpoint")
	}
	if _, err := nilVault.CheckpointFreelists(ctx); err == nil {
		t.Fatal("checkpoint on nil vault succeeded")
	}

	unsupported := openLiveDouble(t)
	if unsupported.SupportsFreelistCheckpoint() {
		t.Fatal("engine without checkpoint capability was accepted")
	}
	if _, err := unsupported.CheckpointFreelists(
		ctx,
	); !errors.Is(
		err,
		vault.ErrFreelistCheckpointUnsupported,
	) {
		t.Fatalf("unsupported checkpoint error = %v", err)
	}

	engine := &freelistCheckpointEngine{
		result: vault.FreelistCheckpointResult{ShardsCheckpointed: 7, ShardsTotal: 8},
	}
	engine.doubleEngine = &doubleEngine{buckets: map[vault.Name]map[string][]byte{}}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("new checkpoint-capable vault: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	if !storage.SupportsFreelistCheckpoint() {
		t.Fatal("checkpoint-capable engine was not detected")
	}
	result, err := storage.CheckpointFreelists(ctx)
	if err != nil || engine.calls != 1 || result.ShardsCheckpointed != 7 ||
		result.ShardsTotal != 8 {
		t.Fatalf("checkpoint result=%+v calls=%d error=%v", result, engine.calls, err)
	}

	engine.err = errCapability
	result, err = storage.CheckpointFreelists(ctx)
	if !errors.Is(err, errCapability) || result.ShardsCheckpointed != 7 || engine.calls != 2 {
		t.Fatalf("failed checkpoint result=%+v calls=%d error=%v", result, engine.calls, err)
	}
}
