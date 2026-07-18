package frontier

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type checkpointRunLedger interface {
	Inspect(context.Context, []byte, []byte) (frontiercheckpoint.RunState, error)
	Begin(context.Context, []byte, []byte, yagocrawlcontract.CrawlOrderPriority) error
	BeginSeedManifest(
		context.Context,
		[]byte,
		[]byte,
		yagocrawlcontract.CrawlOrderPriority,
		[]frontiercheckpoint.Page,
	) error
	Admit(context.Context, []byte, []frontiercheckpoint.Page) (int, error)
	AdmitSeedBatch(
		context.Context,
		[]byte,
		frontiercheckpoint.SeedBatch,
	) (frontiercheckpoint.SeedBatchResult, error)
	FinishSeeding(context.Context, []byte, yagocrawlcontract.CrawlRunTally) error
	Load(context.Context, []byte) (frontiercheckpoint.Snapshot, error)
	Delete(context.Context, []byte) error
}

type checkpointPageLedger interface {
	CompletePage(context.Context, []byte, string, frontiercheckpoint.PageCompletion) error
	RecordRedirect(context.Context, []byte, frontiercheckpoint.Redirect) (bool, error)
}

type checkpointControlLedger interface {
	UpdateControl(context.Context, []byte, frontiercheckpoint.ControlUpdate) error
}

type Checkpoint interface {
	checkpointRunLedger
	checkpointPageLedger
	checkpointControlLedger
}

type boundedRecoveryCheckpoint interface {
	LoadBounded(context.Context, []byte, int) (frontiercheckpoint.Snapshot, error)
	LoadRecoveryPageBatch(
		context.Context,
		[]byte,
		uint64,
		uint64,
		int,
	) (frontiercheckpoint.RecoveryPageBatch, error)
	LoadSeedPageBatch(
		context.Context,
		[]byte,
		uint64,
		int,
	) ([]frontiercheckpoint.Page, uint64, bool, error)
	AdmissionBatchState(
		context.Context,
		[]byte,
		[]frontiercheckpoint.Page,
	) (frontiercheckpoint.AdmissionBatchState, error)
	CancelRecoveryPages(context.Context, []byte, uint64, uint64) (uint64, error)
	FinishSeedingBatch(
		context.Context,
		[]byte,
		yagocrawlcontract.CrawlRunTally,
	) (bool, error)
	CancelSeedManifestBatch(context.Context, []byte) (bool, error)
}

type RunRecovery struct {
	Checkpointed bool
	Completed    bool
	Seeding      bool
	Pages        uint64
	Pending      uint64
	Failed       bool
	Cancelled    bool
	SeedManifest bool
	Tally        yagocrawlcontract.CrawlRunTally
}

func WithCheckpoint(checkpoint Checkpoint) Option {
	return func(frontier *Frontier) {
		frontier.checkpoint = checkpoint
	}
}

func WithCheckpointFailureShutdown(shutdown func()) Option {
	return func(frontier *Frontier) {
		frontier.checkpointShutdown = shutdown
	}
}

func (f *Frontier) Recovery(
	ctx context.Context,
	provenance []byte,
	orderIdentity []byte,
) (RunRecovery, error) {
	if f.checkpoint == nil || len(provenance) == 0 {
		return RunRecovery{}, nil
	}
	state, err := f.checkpoint.Inspect(ctx, provenance, orderIdentity)
	if err != nil {
		f.RecordCheckpointFailure(err)

		return RunRecovery{}, fmt.Errorf("inspect frontier recovery: %w", err)
	}
	if state.Status == frontiercheckpoint.RunMissing {
		return RunRecovery{}, nil
	}

	return RunRecovery{
		Checkpointed: true,
		Completed:    state.Status == frontiercheckpoint.RunCompleted,
		Seeding:      state.Seeding,
		Pages:        state.Pages,
		Pending:      state.Pending,
		Failed:       state.Failed,
		Cancelled:    state.Control.Cancelled,
		SeedManifest: state.SeedManifest,
		Tally:        state.Tally,
	}, nil
}

func (f *Frontier) ForgetCheckpoint(ctx context.Context, provenance []byte) error {
	if f.checkpoint == nil || len(provenance) == 0 {
		return nil
	}
	if err := f.checkpoint.Delete(ctx, provenance); err != nil {
		f.RecordCheckpointFailure(err)

		return fmt.Errorf("forget frontier checkpoint: %w", err)
	}

	return nil
}

func (f *Frontier) CheckpointFailure() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.checkpointFailure
}

func (f *Frontier) RecordCheckpointFailure(err error) {
	if err == nil {
		return
	}
	f.mu.Lock()
	f.recordCheckpointFailureLocked(err)
	f.mu.Unlock()
	f.wake()
}

func (f *Frontier) recordCheckpointFailureLocked(err error) {
	if err == nil || f.checkpointFailure != nil {
		return
	}
	f.checkpointFailure = err
	if f.checkpointShutdown != nil {
		f.checkpointShutdown()
	}
}

func (f *Frontier) persistent(provenance []byte) bool {
	return f.checkpoint != nil && len(provenance) != 0
}
