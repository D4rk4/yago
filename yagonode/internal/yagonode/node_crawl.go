package yagonode

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/crawlruns"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type crawlProcess interface {
	Run(ctx context.Context)
	Close()
}

type crawlRuntime struct {
	exchange    *crawlbroker.CrawlerDataExchangeRuntime
	consumer    *crawlresults.IngestConsumer
	runs        *crawlruns.Registry
	indexImport adminui.IndexImporter
}

var openCrawlerDataExchange = crawlbroker.OpenCrawlerDataExchange

func buildCrawlRuntime(
	ctx context.Context,
	config crawlConfig,
	_ nodeidentity.Identity,
	storage nodeStorage,
	storageVault *vault.Vault,
	dataExchange ...crawlbroker.CrawlerDataExchangeConfig,
) (*crawlRuntime, error) {
	if !config.Enabled() {
		return nil, nil
	}
	var crawlerExchangeConfig crawlbroker.CrawlerDataExchangeConfig
	if len(dataExchange) > 0 {
		crawlerExchangeConfig = dataExchange[0]
	}
	runs := crawlruns.New(0)
	crawlerExchangeConfig.RecordProgress = runs.Record
	exchangeConfig := crawlbroker.Config{
		ListenAddr:            config.ListenAddr,
		StoragePressurePolicy: crawlerStoragePressurePolicy(config),
		RuntimePolicy:         config.RuntimePolicy,
		GrowthAdmission:       config.GrowthAdmission,
		DataExchange:          crawlerExchangeConfig,
	}
	exchange, err := openCrawlerDataExchange(exchangeConfig, storageVault)
	if err != nil {
		return nil, err
	}
	ingest, err := openCrawlIngestRuntime(
		config,
		storage,
		storageVault,
		exchange.Ingest,
	)
	if err != nil {
		exchange.Close()
		return nil, err
	}

	runtime := &crawlRuntime{
		exchange: exchange,
		consumer: ingest.consumer,
		runs:     runs,
	}
	importer := newDocumentImportService(
		storage.documentDirectory,
		exchange.Ingest,
	)
	runtime.indexImport = importer

	return runtime, nil
}

func (r *crawlRuntime) ImportDocuments(
	ctx context.Context,
	records []adminui.IndexImportRecord,
) ([]adminui.IndexImportResult, error) {
	results, err := r.indexImport.ImportDocuments(ctx, records)
	if err != nil {
		return nil, fmt.Errorf("import documents through crawl runtime: %w", err)
	}
	return results, nil
}

func (r *crawlRuntime) observe(observer crawlresults.IngestObserver) {
	r.consumer.Observe(observer)
	r.conditionalFetchIssues, _ = observer.(conditionalFetchIssueObserver)
}

func (r *crawlRuntime) useContentSafetyClassifier(
	classifier crawlresults.ContentSafetyClassifier,
) {
	r.consumer.UseContentSafetyClassifier(classifier)
}

func attachContentSafetyClassifier(
	runtime crawlProcess,
	classifier crawlresults.ContentSafetyClassifier,
) {
	consumer, ok := runtime.(interface {
		useContentSafetyClassifier(crawlresults.ContentSafetyClassifier)
	})
	if ok {
		consumer.useContentSafetyClassifier(classifier)
	}
}

func (r *crawlRuntime) runRegistry() *crawlruns.Registry {
	return r.runs
}

func (r *crawlRuntime) controlRegistry() *crawlbroker.ControlRegistry {
	if r.exchange == nil {
		return nil
	}
	return r.exchange.Control
}

// attachCrawlMetrics wires the crawl ingest observer when the runtime supports it
// and a collector is configured, so crawl throughput is metered only for a live
// crawl runtime and never for a disabled one or a test double.
func attachCrawlMetrics(runtime crawlProcess, collector *metrics.CrawlMetrics) {
	if collector == nil {
		return
	}
	observed, ok := runtime.(interface {
		observe(crawlresults.IngestObserver)
	})
	if !ok {
		return
	}

	observed.observe(collector)
}

func (r *crawlRuntime) Run(ctx context.Context) {
	r.consumer.Run(ctx)
}

func (r *crawlRuntime) Close() {
	if r.exchange != nil {
		r.exchange.Close()
	}
}

func mintProvenance() []byte {
	identity := make([]byte, yagomodel.HashLength)
	_, _ = rand.Read(identity)
	return identity
}
