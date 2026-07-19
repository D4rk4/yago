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
	view := buildIndexStorageStatus(nil, nil)
	if view.NodeVisible {
		t.Fatal("nil storage pressure source became visible")
	}
	unavailable := buildIndexStorageStatus(nil, fixedStoragePressureStatus{
		status: StoragePressureStatus{Pressured: true},
	})
	if !unavailable.NodeVisible || unavailable.NodeAvailable ||
		!strings.Contains(unavailable.NodeText, "unavailable") {
		t.Fatalf("unavailable pressure view = %+v", unavailable)
	}
	healthy := buildIndexStorageStatus(nil, fixedStoragePressureStatus{
		status: StoragePressureStatus{
			AvailableBytes: 2 << 30, ReservedFreeBytes: 1 << 30,
			MeasurementAvailable: true,
		},
	})
	if !healthy.NodeAvailable || healthy.NodePressured ||
		strings.Contains(healthy.NodeText, "recovery margin") {
		t.Fatalf("healthy pressure view = %+v", healthy)
	}
	pressured := buildIndexStorageStatus(nil, fixedStoragePressureStatus{
		status: StoragePressureStatus{
			AvailableBytes: 900 << 20, ReservedFreeBytes: 1 << 30,
			PressureHysteresisBytes: 256 << 20,
			MeasurementAvailable:    true, Pressured: true,
		},
	})
	if !pressured.NodePressured ||
		!strings.Contains(pressured.NodeText, "recovery margin") ||
		!strings.Contains(pressured.NodeText, "ingestion paused") ||
		!strings.Contains(pressured.NodeText, "reusable internal pages") {
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
	disabled := buildIndexStorageStatus(nil, nil)
	if disabled.CrawlerVisible {
		t.Fatal("crawler-disabled storage status became visible")
	}
	unknown := buildIndexStorageStatus(fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers: 1, ActiveFetchesKnown: true,
			FetchLimitPerCrawler: 1, AggregateFetchCapacity: 1,
		},
	}, nil)
	if !unknown.CrawlerVisible || unknown.CrawlerAvailable ||
		unknown.CrawlerPressured ||
		!strings.Contains(unknown.CrawlerText, "not reported") ||
		strings.Contains(unknown.CrawlerText, "paused") {
		t.Fatalf("unknown crawler storage = %+v", unknown)
	}
	reported := buildIndexStorageStatus(fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers: 2, ActiveFetchesKnown: true,
			FetchLimitPerCrawler: 2, AggregateFetchCapacity: 4,
			StorageStatesKnown: true, StorageReportedCrawlers: 2,
			StoragePressured:               1,
			MinimumStorageAvailableBytes:   700 << 20,
			StorageReservedFreeBytes:       1 << 30,
			StoragePressureHysteresisBytes: 128 << 20,
		},
	}, nil)
	if !reported.CrawlerAvailable || !reported.CrawlerPressured ||
		!strings.Contains(reported.CrawlerText, "fetch admission paused") ||
		!strings.Contains(reported.CrawlerText, "free filesystem space") {
		t.Fatalf("reported crawler storage = %+v", reported)
	}
	measurementFailure := buildIndexStorageStatus(fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers: 1, ActiveFetchesKnown: true,
			FetchLimitPerCrawler: 1, AggregateFetchCapacity: 1,
			StorageStatesKnown: true, StorageReportedCrawlers: 1,
			StorageMeasurementsUnavailable: 1,
		},
	}, nil)
	if measurementFailure.CrawlerAvailable || !measurementFailure.CrawlerPressured ||
		!strings.Contains(measurementFailure.CrawlerText, "unavailable") {
		t.Fatalf("crawler measurement failure = %+v", measurementFailure)
	}
	inferredReported := buildIndexStorageStatus(fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers: 1, ActiveFetchesKnown: true,
			FetchLimitPerCrawler: 1, AggregateFetchCapacity: 1,
			StorageStatesKnown: true, MinimumStorageAvailableBytes: 2 << 30,
		},
	}, nil)
	if !inferredReported.CrawlerAvailable ||
		strings.Contains(inferredReported.CrawlerText, "not reported") {
		t.Fatalf("inferred reported crawler storage = %+v", inferredReported)
	}
}

func TestCrawlerStorageStatusPreservesKnownPressureWithLegacyCrawler(t *testing.T) {
	mixed := buildIndexStorageStatus(fixedCrawlerFetchActivity{
		activity: CrawlerFetchActivity{
			ConnectedCrawlers: 2, ActiveFetchesKnown: true,
			FetchLimitPerCrawler: 2, AggregateFetchCapacity: 4,
			StorageReportedCrawlers: 1, StorageUnreportedCrawlers: 1,
			StoragePressured: 1, MinimumStorageAvailableBytes: 700 << 20,
			StorageReservedFreeBytes: 1 << 30,
		},
	}, nil)
	if !mixed.CrawlerAvailable || !mixed.CrawlerPressured ||
		!strings.Contains(mixed.CrawlerText, "1 pressured") ||
		!strings.Contains(mixed.CrawlerText, "1 of 2 not reported") ||
		!strings.Contains(mixed.CrawlerText, "fetch admission paused") {
		t.Fatalf("mixed crawler storage = %+v", mixed)
	}
}
