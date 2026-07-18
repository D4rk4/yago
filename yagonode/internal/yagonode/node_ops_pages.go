package yagonode

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/metrichistory"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// performanceHistorySampleInterval paces the metrics sampler: ninety points at
// ten seconds give the Performance page a fifteen-minute history window.
const (
	performanceHistorySampleInterval = 10 * time.Second
	performanceHistoryCapacity       = 90
)

// performanceHistorySource adapts the metrics sampler to the console's history
// port, converting the sampled series into the console's view types.
type performanceHistorySource struct {
	sampler *metrichistory.Sampler
}

func newPerformanceHistorySource(sampler *metrichistory.Sampler) adminui.PerformanceHistorySource {
	if sampler == nil {
		return nil
	}

	return performanceHistorySource{sampler: sampler}
}

func (s performanceHistorySource) Series() []adminui.HistorySeries {
	sampled := s.sampler.Series()
	out := make([]adminui.HistorySeries, 0, len(sampled))
	for _, series := range sampled {
		points := make([]adminui.HistoryPoint, 0, len(series.Points))
		for _, point := range series.Points {
			points = append(points, adminui.HistoryPoint{At: point.At, Value: point.Value})
		}
		out = append(out, adminui.HistorySeries{
			Kind:   performanceHistoryKind(series.Name),
			Name:   series.Name,
			Unit:   series.Unit,
			Points: points,
		})
	}

	return out
}

func performanceHistoryKind(name string) adminui.HistorySeriesKind {
	switch name {
	case metrichistory.SeriesProcessCPU:
		return adminui.HistorySeriesProcessCPU
	case metrichistory.SeriesProcessMemory:
		return adminui.HistorySeriesProcessMemory
	case metrichistory.SeriesHostMemoryTotal:
		return adminui.HistorySeriesHostMemoryTotal
	case metrichistory.SeriesHostMemoryAvailable:
		return adminui.HistorySeriesHostMemoryAvailable
	case metrichistory.SeriesStorageUse:
		return adminui.HistorySeriesStorageUse
	case metrichistory.SeriesStorageCap:
		return adminui.HistorySeriesStorageCapacity
	default:
		return adminui.HistorySeriesGeneral
	}
}

// backupStatusSource reports the storage status the Backup & restore page
// shows, reading usage from the vault it describes.
type backupStatusSource struct {
	vault   *vault.Vault
	dataDir string
}

func newBackupStatusSource(v *vault.Vault, dataDir string) adminui.BackupSource {
	if v == nil {
		return nil
	}

	return backupStatusSource{vault: v, dataDir: dataDir}
}

func (s backupStatusSource) BackupStatus(ctx context.Context) (adminui.BackupStatus, error) {
	used, err := s.vault.UsedBytes(ctx)
	if err != nil {
		return adminui.BackupStatus{}, fmt.Errorf("read storage usage: %w", err)
	}

	return adminui.BackupStatus{
		DataDir:    s.dataDir,
		UsedBytes:  used,
		QuotaBytes: s.vault.QuotaBytes(),
	}, nil
}

// gateRestartControls strips the console's restart controls when the operator
// disabled them by configuration (UI-09 acceptance): the pages then render as
// unavailable instead of offering the actions.
func gateRestartControls(options *adminui.Options, enabled bool) {
	if enabled {
		return
	}
	options.Restart = nil
	options.RestartCrawlers = nil
}

// applyOpsPageOptions attaches the UI-09 operational pages — the sampled
// performance history, the backup status, and the restart-control gate — to the
// console options, keeping buildOpsMux within its length budget.
func applyOpsPageOptions(
	options *adminui.Options,
	config nodeConfig,
	assembled node,
	sources consoleAdminSources,
) {
	options.PerformanceHistory = sources.perfHistory
	options.Backup = newBackupStatusSource(assembled.vault, config.DataDir)
	options.StoragePressure = newStoragePressureStatusSource(assembled.storagePressure)
	gateRestartControls(options, config.AdminRestartEnabled)
}

// applyCrawlAdminOptions wires the crawl dispatch, monitor, and control
// sources into the console options when the crawl runtime provides them,
// keeping buildOpsMux within its length budget.
func applyCrawlAdminOptions(
	options *adminui.Options,
	assembled node,
	crawlDepth crawlQueueDepthSource,
) {
	attachCrawlRuntimeSettings(assembled.crawl, assembled.toggles)
	dispatcher := crawlDispatcher(assembled.crawl)
	if dispatcher != nil {
		options.Crawl = newCrawlSource(dispatcher)
		options.Schedules = newCrawlScheduleSource(assembled.schedules, options.Crawl)
	}
	if registry := crawlRunRegistry(assembled.crawl); registry != nil {
		options.Monitor = newCrawlMonitorSource(registry, crawlDepth.probe)
		if control := crawlControlRegistry(assembled.crawl); control != nil {
			options.Control = newCrawlControlSource(
				registry,
				control,
				crawlRestartSource(dispatcher),
			)
		}
	}
	if control := crawlControlRegistry(assembled.crawl); control != nil {
		options.CrawlerFetchActivity = newCrawlerFetchActivitySource(control)
		options.RestartCrawlers = control.RestartWorkers
	}
}
