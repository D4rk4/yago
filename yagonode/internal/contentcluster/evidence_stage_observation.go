package contentcluster

import "time"

type EvidenceStage string

const (
	EvidenceStagePreparation        EvidenceStage = "content_cluster_evidence_preparation"
	EvidenceStageLeaseAcquisition   EvidenceStage = "content_cluster_lease_acquisition"
	EvidenceStageTransitionRead     EvidenceStage = "content_cluster_transition_read"
	EvidenceStageTransitionRecovery EvidenceStage = "content_cluster_transition_recovery"
	EvidenceStageCandidatePlanning  EvidenceStage = "content_cluster_candidate_planning"
	EvidenceStageProjectionLeasing  EvidenceStage = "content_cluster_projection_lease_expansion"
	EvidenceStageTransitionCommit   EvidenceStage = "content_cluster_transition_commit"
	EvidenceStageOutputCompletion   EvidenceStage = "content_cluster_output_completion"
)

type EvidenceStageObserver interface {
	ObserveContentClusterStage(EvidenceStage, time.Duration)
}

func measureEvidenceStage(
	observer EvidenceStageObserver,
	stage EvidenceStage,
) func() {
	started := time.Now()
	return func() {
		if observer != nil {
			observer.ObserveContentClusterStage(stage, time.Since(started))
		}
	}
}
