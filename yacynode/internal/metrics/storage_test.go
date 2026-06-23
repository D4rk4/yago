package metrics

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type stubStorage struct {
	quota int64
	used  int64
	err   error
}

func (s stubStorage) QuotaBytes() int64 { return s.quota }

func (s stubStorage) UsedBytes(context.Context) (int64, error) { return s.used, s.err }

func TestStorageReportsLevels(t *testing.T) {
	storage := NewStorageMetrics(prometheus.NewRegistry(), stubStorage{quota: 1024, used: 256})

	if got := testutil.ToFloat64(storage.quota); got != 1024 {
		t.Errorf("quota bytes = %v, want 1024", got)
	}
	if got := testutil.ToFloat64(storage.used); got != 256 {
		t.Errorf("used bytes = %v, want 256", got)
	}
}

func TestStorageReportsZeroUsedOnError(t *testing.T) {
	storage := NewStorageMetrics(
		prometheus.NewRegistry(),
		stubStorage{used: 256, err: errors.New("unavailable")},
	)

	if got := testutil.ToFloat64(storage.used); got != 0 {
		t.Errorf("used bytes = %v, want 0 on error", got)
	}
}
