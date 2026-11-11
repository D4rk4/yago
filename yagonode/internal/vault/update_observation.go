package vault

import "time"

type UpdatePhase string

const (
	UpdatePhaseReadYield        UpdatePhase = "read_yield"
	UpdatePhaseWriterAdmission  UpdatePhase = "writer_admission"
	UpdatePhaseGlobalGate       UpdatePhase = "global_gate"
	UpdatePhaseTransaction      UpdatePhase = "transaction"
	UpdatePhaseShardTransaction UpdatePhase = "shard_transaction"
	UpdatePhaseCommitValidation UpdatePhase = "commit_validation"
	UpdatePhaseShardCommit      UpdatePhase = "shard_commit"
	UpdatePhaseCommit           UpdatePhase = "commit"
	UpdatePhaseTotal            UpdatePhase = "update_total"
)

type UpdateAttempt string

const (
	UpdateAttemptInitial    UpdateAttempt = "initial"
	UpdateAttemptConcurrent UpdateAttempt = "concurrent"
	UpdateAttemptContended  UpdateAttempt = "contended"
	UpdateAttemptComplete   UpdateAttempt = "complete"
)

type VaultUpdateObserver interface {
	ObserveVaultUpdate(UpdatePhase, UpdateAttempt, time.Duration)
}
