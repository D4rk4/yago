package adminui

import (
	"fmt"
	"strings"
)

type CrawlerFetchActivity struct {
	ConnectedCrawlers              int
	ActiveFetches                  int
	FetchLimitPerCrawler           int
	AggregateFetchCapacity         int
	ActiveFetchesKnown             bool
	StorageStatesKnown             bool
	StorageReportedCrawlers        int
	StorageUnreportedCrawlers      int
	StoragePressured               int
	StorageMeasurementsUnavailable int
	MinimumStorageAvailableBytes   uint64
	StorageReservedFreeBytes       uint64
	StoragePressureHysteresisBytes uint64
}

type CrawlerFetchActivitySource interface {
	CrawlerFetchActivity() CrawlerFetchActivity
}

func applyCrawlerFetchActivity(
	view systemMonitorView,
	source CrawlerFetchActivitySource,
) systemMonitorView {
	if source == nil {
		return view
	}
	activity := source.CrawlerFetchActivity()
	if activity.ConnectedCrawlers <= 0 {
		return view
	}
	view.CrawlerFetchVisible = true
	view.CrawlerStorageVisible = true
	applyCrawlerStorageActivity(&view, activity)
	if !validCrawlerFetchActivity(activity) {
		return view
	}

	view.CrawlerFetchAvailable = true
	view.CrawlerFetchValue = min(activity.ActiveFetches, activity.AggregateFetchCapacity)
	view.CrawlerFetchMaximum = activity.AggregateFetchCapacity
	view.CrawlerFetchText = fmt.Sprintf(
		"%d active of %d",
		activity.ActiveFetches,
		activity.AggregateFetchCapacity,
	)
	if activity.ConnectedCrawlers > 1 {
		view.CrawlerFetchText += fmt.Sprintf(
			" · %d crawlers × %d each",
			activity.ConnectedCrawlers,
			activity.FetchLimitPerCrawler,
		)
	}

	return view
}

func applyCrawlerStorageActivity(view *systemMonitorView, activity CrawlerFetchActivity) {
	reported := activity.StorageReportedCrawlers
	unreported := activity.StorageUnreportedCrawlers
	if reported == 0 && unreported == 0 {
		if activity.StorageStatesKnown {
			reported = activity.ConnectedCrawlers
		} else {
			unreported = activity.ConnectedCrawlers
		}
	}
	if reported == 0 {
		view.CrawlerStorageText = "Storage status not reported"

		return
	}
	parts := make([]string, 0, 5)
	availableReports := reported - activity.StorageMeasurementsUnavailable
	if availableReports > 0 {
		view.CrawlerStorageAvailable = true
		parts = append(parts, storagePressureBytes(activity.MinimumStorageAvailableBytes)+
			" minimum available · reserve "+
			storagePressureBytes(activity.StorageReservedFreeBytes))
		if activity.StoragePressureHysteresisBytes > 0 {
			parts[len(parts)-1] += " + " +
				storagePressureBytes(activity.StoragePressureHysteresisBytes) +
				" recovery margin"
		}
	}
	if activity.StorageMeasurementsUnavailable > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d measurement unavailable",
			activity.StorageMeasurementsUnavailable,
		))
	}
	if activity.StoragePressured > 0 {
		parts = append(parts, fmt.Sprintf("%d pressured", activity.StoragePressured))
	}
	if unreported > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d of %d not reported",
			unreported,
			activity.ConnectedCrawlers,
		))
	}
	view.CrawlerStoragePressured = activity.StoragePressured > 0 ||
		activity.StorageMeasurementsUnavailable > 0
	if view.CrawlerStoragePressured {
		parts = append(parts, "frontier growth and fetch admission paused")
		parts = append(parts,
			"free filesystem space or lower reserve and recovery margin; "+
				"bbolt deletion may only create reusable internal pages",
		)
	}
	view.CrawlerStorageText = strings.Join(parts, " · ")
}

func validCrawlerFetchActivity(activity CrawlerFetchActivity) bool {
	if !activity.ActiveFetchesKnown || activity.ConnectedCrawlers <= 0 ||
		activity.ActiveFetches < 0 || activity.FetchLimitPerCrawler <= 0 {
		return false
	}
	connected := int64(activity.ConnectedCrawlers)
	perCrawler := int64(activity.FetchLimitPerCrawler)
	capacity := int64(activity.AggregateFetchCapacity)

	return capacity/perCrawler == connected && capacity%perCrawler == 0
}
