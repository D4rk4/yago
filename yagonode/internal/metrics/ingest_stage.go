package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type IngestStageMetrics struct {
	duration *prometheus.HistogramVec
}

func NewIngestStageMetrics(registry prometheus.Registerer) *IngestStageMetrics {
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "crawl_ingest_stage_duration_seconds",
		Help:    "Duration of bounded crawl-ingest stages in seconds.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 180, 300, 600},
	}, []string{"stage"})
	registry.MustRegister(duration)

	return &IngestStageMetrics{duration: duration}
}

func (m *IngestStageMetrics) ObserveIngestStage(
	stage crawlresults.IngestStage,
	elapsed time.Duration,
) {
	if !knownIngestStage(stage) {
		return
	}
	m.duration.WithLabelValues(string(stage)).Observe(elapsed.Seconds())
}

func knownIngestStage(stage crawlresults.IngestStage) bool {
	switch stage {
	case crawlresults.IngestStageQueueWait,
		crawlresults.IngestStageConsumerBatch,
		crawlresults.IngestStageDeliveryAdmission,
		crawlresults.IngestStageStorageAdmission,
		crawlresults.IngestStageDocumentWrite,
		crawlresults.IngestStageSearchIndexWrite,
		crawlresults.IngestStageURLMetadataWrite,
		crawlresults.IngestStageStalePostingSweep,
		crawlresults.IngestStageRWIPostingWrite,
		crawlresults.IngestStageDeliveryAuthorization,
		crawlresults.IngestStageObservationHistory,
		crawlresults.IngestStageLineageReservation,
		crawlresults.IngestStageDocumentCanonicalization,
		crawlresults.IngestStageContentClusterPreparation,
		crawlresults.IngestStageContentClusterReplacement,
		crawlresults.IngestStageContentClusterProjection,
		crawlresults.IngestStageContentClusterEvidencePreparation,
		crawlresults.IngestStageContentClusterLeaseAcquisition,
		crawlresults.IngestStageContentClusterTransitionRead,
		crawlresults.IngestStageContentClusterTransitionRecovery,
		crawlresults.IngestStageContentClusterCandidatePlanning,
		crawlresults.IngestStageContentClusterProjectionLeasing,
		crawlresults.IngestStageContentClusterTransitionCommit,
		crawlresults.IngestStageContentClusterOutputCompletion,
		crawlresults.IngestStageContentClusterFinalization,
		crawlresults.IngestStageOutboundAnchorUpdate,
		crawlresults.IngestStageOutboundAnchorDocumentReplacement,
		crawlresults.IngestStageOutboundAnchorIndexCallback,
		crawlresults.IngestStageOutboundAnchorProjection,
		crawlresults.IngestStageOutboundAnchorIndexVisit,
		crawlresults.IngestStageOutboundAnchorFinalization,
		crawlresults.IngestStageRecrawlSchedule:
		return true
	default:
		return false
	}
}
