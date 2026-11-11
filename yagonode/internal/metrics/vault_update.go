package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type VaultUpdateMetrics struct {
	duration *prometheus.HistogramVec
}

func NewVaultUpdateMetrics(registry prometheus.Registerer) *VaultUpdateMetrics {
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "vault_update_phase_duration_seconds",
		Help: "Duration of bounded vault update phases; shard transaction and commit phases are measured per touched shard.",
		Buckets: []float64{
			0.0001, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5,
			5, 10, 30, 60, 120, 180, 300, 600,
		},
	}, []string{"phase", "attempt"})
	registry.MustRegister(duration)

	return &VaultUpdateMetrics{duration: duration}
}

func (m *VaultUpdateMetrics) ObserveVaultUpdate(
	phase vault.UpdatePhase,
	attempt vault.UpdateAttempt,
	elapsed time.Duration,
) {
	if !knownVaultUpdatePhase(phase) || !knownVaultUpdateAttempt(attempt) {
		return
	}
	m.duration.WithLabelValues(string(phase), string(attempt)).Observe(elapsed.Seconds())
}

func knownVaultUpdatePhase(phase vault.UpdatePhase) bool {
	switch phase {
	case vault.UpdatePhaseReadYield,
		vault.UpdatePhaseWriterAdmission,
		vault.UpdatePhaseGlobalGate,
		vault.UpdatePhaseTransaction,
		vault.UpdatePhaseShardTransaction,
		vault.UpdatePhaseCommitValidation,
		vault.UpdatePhaseShardCommit,
		vault.UpdatePhaseCommit,
		vault.UpdatePhaseTotal:
		return true
	default:
		return false
	}
}

func knownVaultUpdateAttempt(attempt vault.UpdateAttempt) bool {
	switch attempt {
	case vault.UpdateAttemptInitial,
		vault.UpdateAttemptConcurrent,
		vault.UpdateAttemptContended,
		vault.UpdateAttemptComplete:
		return true
	default:
		return false
	}
}
