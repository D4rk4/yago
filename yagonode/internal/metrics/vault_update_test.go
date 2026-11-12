package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestVaultUpdateMetricsBoundPhasesAndAttempts(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	observer := NewVaultUpdateMetrics(registry)
	observer.ObserveVaultUpdate(
		vault.UpdatePhaseShardCommit,
		vault.UpdateAttemptContended,
		250*time.Millisecond,
	)
	observer.ObserveVaultUpdate("unbounded-phase", vault.UpdateAttemptComplete, time.Second)
	observer.ObserveVaultUpdate(vault.UpdatePhaseCommit, "unbounded-attempt", time.Second)

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	if len(families) != 1 || families[0].GetName() != "vault_update_phase_duration_seconds" {
		t.Fatalf("metric families = %v, want one vault update duration family", families)
	}
	family := families[0]
	if len(family.Metric) != 1 {
		t.Fatalf("metric series = %d, want 1", len(family.Metric))
	}
	metric := family.Metric[0]
	if metric.GetHistogram().GetSampleCount() != 1 ||
		metric.GetHistogram().GetSampleSum() != 0.25 {
		t.Fatalf("observed shard commit = %v, want one 0.25 second sample", metric.GetHistogram())
	}
	labels := map[string]string{}
	for _, label := range metric.Label {
		labels[label.GetName()] = label.GetValue()
	}
	if labels["phase"] != string(vault.UpdatePhaseShardCommit) ||
		labels["attempt"] != string(vault.UpdateAttemptContended) {
		t.Fatalf("labels = %v, want bounded shard commit and contended values", labels)
	}
}
