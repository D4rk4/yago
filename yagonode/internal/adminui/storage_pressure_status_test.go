package adminui

import (
	"math"
	"strings"
	"testing"
)

type fixedStoragePressureStatus struct {
	status StoragePressureStatus
}

func (s fixedStoragePressureStatus) StoragePressureStatus() StoragePressureStatus {
	return s.status
}

func TestStoragePressureStatusRendersHealthyPressuredAndUnavailable(t *testing.T) {
	view := applyStoragePressureStatus(systemMonitorView{}, nil)
	if view.NodeStoragePressureVisible {
		t.Fatal("nil storage pressure source became visible")
	}
	unavailable := applyStoragePressureStatus(systemMonitorView{}, fixedStoragePressureStatus{
		status: StoragePressureStatus{Pressured: true},
	})
	if !unavailable.NodeStoragePressureVisible || unavailable.NodeStoragePressureAvailable ||
		!strings.Contains(unavailable.NodeStoragePressureText, "unavailable") {
		t.Fatalf("unavailable pressure view = %+v", unavailable)
	}
	healthy := applyStoragePressureStatus(systemMonitorView{}, fixedStoragePressureStatus{
		status: StoragePressureStatus{
			AvailableBytes: 2 << 30, ReservedFreeBytes: 1 << 30,
			MeasurementAvailable: true,
		},
	})
	if !healthy.NodeStoragePressureAvailable || healthy.NodeStoragePressured ||
		strings.Contains(healthy.NodeStoragePressureText, "recovery margin") {
		t.Fatalf("healthy pressure view = %+v", healthy)
	}
	pressured := applyStoragePressureStatus(systemMonitorView{}, fixedStoragePressureStatus{
		status: StoragePressureStatus{
			AvailableBytes: 900 << 20, ReservedFreeBytes: 1 << 30,
			PressureHysteresisBytes: 256 << 20,
			MeasurementAvailable:    true, Pressured: true,
		},
	})
	if !pressured.NodeStoragePressured ||
		!strings.Contains(pressured.NodeStoragePressureText, "recovery margin") ||
		!strings.Contains(pressured.NodeStoragePressureText, "ingestion paused") ||
		!strings.Contains(pressured.NodeStoragePressureText, "reusable internal pages") {
		t.Fatalf("pressured view = %+v", pressured)
	}
	if got := storagePressureBytes(math.MaxUint64); got == "" {
		t.Fatal("maximum storage size formatted empty")
	}
	if got := storagePressureBytes(0); got != "0 B" {
		t.Fatalf("zero storage size = %q", got)
	}
}

func TestCrawlerStorageStatusRequiresConnectedReportedCrawler(t *testing.T) {
	disabled := buildSystemMonitorWithCrawler(nil, nil)
	if disabled.CrawlerStorageVisible {
		t.Fatal("crawler-disabled storage status became visible")
	}
	unknown := applyCrawlerFetchActivity(systemMonitorView{}, fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers: 1, ActiveFetchesKnown: true,
			FetchLimitPerCrawler: 1, AggregateFetchCapacity: 1,
		},
	})
	if !unknown.CrawlerStorageVisible || unknown.CrawlerStorageAvailable ||
		unknown.CrawlerStoragePressured ||
		!strings.Contains(unknown.CrawlerStorageText, "not reported") ||
		strings.Contains(unknown.CrawlerStorageText, "paused") {
		t.Fatalf("unknown crawler storage = %+v", unknown)
	}
	reported := applyCrawlerFetchActivity(systemMonitorView{}, fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers: 2, ActiveFetchesKnown: true,
			FetchLimitPerCrawler: 2, AggregateFetchCapacity: 4,
			StorageStatesKnown: true, StorageReportedCrawlers: 2,
			StoragePressured:               1,
			MinimumStorageAvailableBytes:   700 << 20,
			StorageReservedFreeBytes:       1 << 30,
			StoragePressureHysteresisBytes: 128 << 20,
		},
	})
	if !reported.CrawlerStorageAvailable || !reported.CrawlerStoragePressured ||
		!strings.Contains(reported.CrawlerStorageText, "fetch admission paused") ||
		!strings.Contains(reported.CrawlerStorageText, "free filesystem space") {
		t.Fatalf("reported crawler storage = %+v", reported)
	}
	measurementFailure := applyCrawlerFetchActivity(systemMonitorView{}, fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers: 1, ActiveFetchesKnown: true,
			FetchLimitPerCrawler: 1, AggregateFetchCapacity: 1,
			StorageStatesKnown: true, StorageReportedCrawlers: 1,
			StorageMeasurementsUnavailable: 1,
		},
	})
	if measurementFailure.CrawlerStorageAvailable || !measurementFailure.CrawlerStoragePressured ||
		!strings.Contains(measurementFailure.CrawlerStorageText, "unavailable") {
		t.Fatalf("crawler measurement failure = %+v", measurementFailure)
	}
	inferredReported := applyCrawlerFetchActivity(systemMonitorView{}, fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers: 1, ActiveFetchesKnown: true,
			FetchLimitPerCrawler: 1, AggregateFetchCapacity: 1,
			StorageStatesKnown: true, MinimumStorageAvailableBytes: 2 << 30,
		},
	})
	if !inferredReported.CrawlerStorageAvailable ||
		strings.Contains(inferredReported.CrawlerStorageText, "not reported") {
		t.Fatalf("inferred reported crawler storage = %+v", inferredReported)
	}
}

func TestCrawlerStorageStatusPreservesKnownPressureWithLegacyCrawler(t *testing.T) {
	mixed := applyCrawlerFetchActivity(systemMonitorView{}, fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers: 2, ActiveFetchesKnown: true,
			FetchLimitPerCrawler: 2, AggregateFetchCapacity: 4,
			StorageReportedCrawlers: 1, StorageUnreportedCrawlers: 1,
			StoragePressured: 1, MinimumStorageAvailableBytes: 700 << 20,
			StorageReservedFreeBytes: 1 << 30,
		},
	})
	if !mixed.CrawlerStorageAvailable || !mixed.CrawlerStoragePressured ||
		!strings.Contains(mixed.CrawlerStorageText, "1 pressured") ||
		!strings.Contains(mixed.CrawlerStorageText, "1 of 2 not reported") ||
		!strings.Contains(mixed.CrawlerStorageText, "fetch admission paused") {
		t.Fatalf("mixed crawler storage = %+v", mixed)
	}
}
