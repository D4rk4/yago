package adminui

import (
	"fmt"
	"strings"
)

type indexStorageStatus struct {
	CrawlerVisible   bool
	CrawlerAvailable bool
	CrawlerPressured bool
	CrawlerText      string
	NodeVisible      bool
	NodeAvailable    bool
	NodePressured    bool
	NodeText         string
}

func buildIndexStorageStatus(
	crawler CrawlerFetchActivitySource,
	node StoragePressureStatusSource,
) indexStorageStatus {
	view := indexStorageStatus{}
	if crawler != nil {
		activity := crawler.CrawlerFetchActivity()
		if activity.ConnectedCrawlers > 0 {
			view.CrawlerVisible = true
			applyIndexCrawlerStorageStatus(&view, activity)
		}
	}

	return applyIndexNodeStorageStatus(view, node)
}

func applyIndexCrawlerStorageStatus(
	view *indexStorageStatus,
	activity CrawlerFetchActivity,
) {
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
		view.CrawlerText = "Storage status not reported"

		return
	}
	parts := make([]string, 0, 5)
	availableReports := reported - activity.StorageMeasurementsUnavailable
	if availableReports > 0 {
		view.CrawlerAvailable = true
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
	view.CrawlerPressured = activity.StoragePressured > 0 ||
		activity.StorageMeasurementsUnavailable > 0
	if view.CrawlerPressured {
		parts = append(parts, "frontier growth and fetch admission paused")
		parts = append(parts,
			"free filesystem space or lower reserve and recovery margin; "+
				"bbolt deletion may only create reusable internal pages",
		)
	}
	view.CrawlerText = strings.Join(parts, " · ")
}
