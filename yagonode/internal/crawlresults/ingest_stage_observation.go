package crawlresults

import (
	"time"

	"github.com/D4rk4/yago/yagonode/internal/contentcluster"
)

type IngestStage string

const (
	IngestStageQueueWait                         IngestStage = "queue_wait"
	IngestStageConsumerBatch                     IngestStage = "consumer_batch"
	IngestStageDeliveryAdmission                 IngestStage = "delivery_admission"
	IngestStageStorageAdmission                  IngestStage = "storage_admission"
	IngestStageDocumentWrite                     IngestStage = "document_write"
	IngestStageSearchIndexWrite                  IngestStage = "search_index_write"
	IngestStageURLMetadataWrite                  IngestStage = "url_metadata_write"
	IngestStageStalePostingSweep                 IngestStage = "stale_posting_sweep"
	IngestStageRWIPostingWrite                   IngestStage = "rwi_posting_write"
	IngestStageDeliveryAuthorization             IngestStage = "delivery_authorization"
	IngestStageObservationHistory                IngestStage = "observation_history"
	IngestStageLineageReservation                IngestStage = "lineage_reservation"
	IngestStageDocumentCanonicalization          IngestStage = "document_canonicalization"
	IngestStageContentClusterPreparation         IngestStage = "content_cluster_preparation"
	IngestStageContentClusterReplacement         IngestStage = "content_cluster_replacement"
	IngestStageContentClusterProjection          IngestStage = "content_cluster_projection_refresh"
	IngestStageContentClusterEvidencePreparation IngestStage = IngestStage(contentcluster.EvidenceStagePreparation)
	IngestStageContentClusterLeaseAcquisition    IngestStage = IngestStage(contentcluster.EvidenceStageLeaseAcquisition)
	IngestStageContentClusterTransitionRead      IngestStage = IngestStage(contentcluster.EvidenceStageTransitionRead)
	IngestStageContentClusterTransitionRecovery  IngestStage = IngestStage(contentcluster.EvidenceStageTransitionRecovery)
	IngestStageContentClusterCandidatePlanning   IngestStage = IngestStage(contentcluster.EvidenceStageCandidatePlanning)
	IngestStageContentClusterProjectionLeasing   IngestStage = IngestStage(contentcluster.EvidenceStageProjectionLeasing)
	IngestStageContentClusterTransitionCommit    IngestStage = IngestStage(contentcluster.EvidenceStageTransitionCommit)
	IngestStageContentClusterOutputCompletion    IngestStage = IngestStage(contentcluster.EvidenceStageOutputCompletion)
	IngestStageContentClusterFinalization        IngestStage = "content_cluster_finalization"
	IngestStageOutboundAnchorUpdate              IngestStage = "outbound_anchor_update"
	IngestStageOutboundAnchorDocumentReplacement IngestStage = "outbound_anchor_document_replacement"
	IngestStageOutboundAnchorIndexCallback       IngestStage = "outbound_anchor_index_callback"
	IngestStageOutboundAnchorProjection          IngestStage = "outbound_anchor_projection"
	IngestStageOutboundAnchorIndexVisit          IngestStage = "outbound_anchor_index_visit"
	IngestStageOutboundAnchorFinalization        IngestStage = "outbound_anchor_finalization"
	IngestStageRecrawlSchedule                   IngestStage = "recrawl_schedule"
)

type IngestStageObserver interface {
	ObserveIngestStage(IngestStage, time.Duration)
}

func (c *IngestConsumer) ObserveIngestStages(observer IngestStageObserver) {
	if observer != nil {
		c.ingestStages = observer
	}
}

func (c *IngestConsumer) observeIngestStage(stage IngestStage, elapsed time.Duration) {
	if c.ingestStages != nil {
		c.ingestStages.ObserveIngestStage(stage, elapsed)
	}
}

func (c *IngestConsumer) measureIngestStage(stage IngestStage) func() {
	started := time.Now()
	return func() {
		c.observeIngestStage(stage, time.Since(started))
	}
}

func (c *IngestConsumer) observeSubmittedIngestQueue(group []IngestDelivery) {
	now := time.Now()
	for _, delivery := range group {
		if delivery.SubmittedAt.IsZero() {
			continue
		}
		c.observeIngestStage(IngestStageQueueWait, now.Sub(delivery.SubmittedAt))
	}
}
