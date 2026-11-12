package yagonode

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/contentquality"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/eviction"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type crawlIngestRuntime struct {
	consumer *crawlresults.IngestConsumer
}

func openCrawlIngestRuntime(
	config crawlConfig,
	storage nodeStorage,
	storageVault *vault.Vault,
	ingestReceiver *crawlbroker.IngestReceiver,
) (crawlIngestRuntime, error) {
	observationHistory, err := crawlresults.OpenURLObservationHistory(storageVault)
	if err != nil {
		return crawlIngestRuntime{}, fmt.Errorf("open crawl observation history: %w", err)
	}
	consumer := crawlresults.NewIngestConsumerWithIndex(
		ingestReceiver,
		storage.documentReceiver,
		storage.searchIndex,
		storage.urlReceiver,
		storage.postingReceiver,
	)
	consumer.AdmitGrowth(crawlStateLifecycleAdmission(config.GrowthAdmission))
	consumer.OrderObservations(observationHistory)
	consumer.TrackContentClusters(storage.contentClusters)
	evictor := eviction.NewEvictor(
		storageVault,
		storage.postingPurger,
		storage.references,
		storage.urlEvictor,
		storage.documentEvictor(),
		storage.urlDirectory,
	)
	consumer.PurgeURLs(evictor)
	consumer.SweepStalePostings(evictor)
	if config.QualityGate {
		consumer.GateQuality(contentquality.RejectionRule)
	}

	return crawlIngestRuntime{consumer: consumer}, nil
}
