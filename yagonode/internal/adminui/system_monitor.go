package adminui

import (
	"fmt"
	"math"
	"runtime"
	"time"
)

const systemMonitorPath = "/admin/system-monitor"

type systemMonitorView struct {
	CrawlerFetchVisible    bool
	CrawlerFetchAvailable  bool
	CrawlerFetchValue      int
	CrawlerFetchMaximum    int
	CrawlerFetchText       string
	CPUAvailable           bool
	CPUValue               float64
	CPUMaximum             float64
	CPUText                string
	ProcessMemoryAvailable bool
	ProcessMemoryValue     int64
	ProcessMemoryMaximum   int64
	ProcessMemoryText      string
	HostMemoryAvailable    bool
	HostMemoryValue        int64
	HostMemoryMaximum      int64
	HostMemoryText         string
	StorageAvailable       bool
	StorageBounded         bool
	StorageValue           int64
	StorageMaximum         int64
	StorageText            string
	Observed               bool
	ObservedAt             string
}

func buildSystemMonitor(source PerformanceHistorySource) systemMonitorView {
	return buildSystemMonitorForProcessors(source, runtime.NumCPU())
}

func buildSystemMonitorWithCrawler(
	source PerformanceHistorySource,
	crawler CrawlerFetchActivitySource,
) systemMonitorView {
	return applyCrawlerFetchActivity(buildSystemMonitor(source), crawler)
}

func buildSystemMonitorForProcessors(
	source PerformanceHistorySource,
	processors int,
) systemMonitorView {
	if source == nil {
		return systemMonitorView{}
	}
	series := source.Series()
	newest := newestHistoryObservation(series)
	if newest.IsZero() {
		return systemMonitorView{}
	}

	view := systemMonitorView{}
	if value, available := currentHistoryValue(
		series,
		HistorySeriesProcessCPU,
		newest,
	); available &&
		processors > 0 && validSystemMeasurement(value) && value <= float64(processors) {
		view.CPUAvailable = true
		view.CPUValue = value
		view.CPUMaximum = float64(processors)
		view.CPUText = fmt.Sprintf("%s of %d logical CPUs", formatHistoryValue(value), processors)
	}
	resident, residentAvailable := currentHistoryValue(
		series,
		HistorySeriesProcessMemory,
		newest,
	)
	total, totalAvailable := currentHistoryValue(
		series,
		HistorySeriesHostMemoryTotal,
		newest,
	)
	available, availableObserved := currentHistoryValue(
		series,
		HistorySeriesHostMemoryAvailable,
		newest,
	)
	residentBytes, residentValid := systemMonitorBytes(resident)
	totalBytes, totalValid := systemMonitorBytes(total)
	availableBytes, availableValid := systemMonitorBytes(available)
	if residentAvailable && totalAvailable && residentValid && totalValid && totalBytes > 0 {
		view.ProcessMemoryAvailable = true
		view.ProcessMemoryValue = min(residentBytes, totalBytes)
		view.ProcessMemoryMaximum = totalBytes
		view.ProcessMemoryText = formatByteSize(residentBytes) +
			" RSS / " + formatByteSize(totalBytes)
	}
	if totalAvailable && availableObserved && totalValid && availableValid &&
		totalBytes > 0 && availableBytes <= totalBytes {
		view.HostMemoryAvailable = true
		view.HostMemoryValue = totalBytes - availableBytes
		view.HostMemoryMaximum = totalBytes
		view.HostMemoryText = formatByteSize(view.HostMemoryValue) + " / " +
			formatByteSize(totalBytes) + " · " + formatByteSize(availableBytes) + " available"
	}
	used, usedAvailable := currentHistoryValue(series, HistorySeriesStorageUse, newest)
	quota, quotaAvailable := currentHistoryValue(series, HistorySeriesStorageCapacity, newest)
	usedBytes, usedValid := systemMonitorBytes(used)
	quotaBytes, quotaValid := systemMonitorBytes(quota)
	if usedAvailable && quotaAvailable && usedValid && quotaValid {
		view.StorageAvailable = true
		view.StorageValue = usedBytes
		view.StorageMaximum = quotaBytes
		view.StorageText = formatByteSize(usedBytes) + " / " + formatStorageQuota(quotaBytes)
		if quotaBytes > 0 {
			view.StorageBounded = true
			view.StorageValue = min(usedBytes, quotaBytes)
		}
	}
	if view.CPUAvailable || view.ProcessMemoryAvailable || view.HostMemoryAvailable ||
		view.StorageAvailable {
		view.Observed = true
		view.ObservedAt = newest.UTC().Format(time.RFC3339)
	}

	return view
}

func newestHistoryObservation(series []HistorySeries) time.Time {
	var newest time.Time
	for _, readings := range series {
		for _, point := range readings.Points {
			if point.At.After(newest) {
				newest = point.At
			}
		}
	}

	return newest
}

func currentHistoryValue(
	series []HistorySeries,
	kind HistorySeriesKind,
	observedAt time.Time,
) (float64, bool) {
	for _, readings := range series {
		if readings.Kind != kind || len(readings.Points) == 0 {
			continue
		}
		point := readings.Points[len(readings.Points)-1]
		if point.At.Equal(observedAt) {
			return point.Value, true
		}
	}

	return 0, false
}

func validSystemMeasurement(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func systemMonitorBytes(value float64) (int64, bool) {
	if !validSystemMeasurement(value) || value > float64(1<<62) {
		return 0, false
	}

	return int64(math.Round(value)), true
}
