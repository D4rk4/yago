package yagonode

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/metrichistory"
)

func TestPerformanceHistorySourceAdaptsSampler(t *testing.T) {
	if newPerformanceHistorySource(nil) != nil {
		t.Fatal("a nil sampler must yield a nil source")
	}

	registry := prometheus.NewRegistry()
	requests := prometheus.NewCounter(prometheus.CounterOpts{Name: "http_requests_total"})
	registry.MustRegister(requests)
	sampler := metrichistory.New(registry, 4, nil)
	source := newPerformanceHistorySource(sampler)

	sampler.Sample()
	time.Sleep(5 * time.Millisecond)
	requests.Add(3)
	sampler.Sample()

	series := source.Series()
	if len(series) == 0 {
		t.Fatal("adapted series missing")
	}
	var found bool
	for _, entry := range series {
		if entry.Name != metrichistory.SeriesRequests {
			continue
		}
		found = true
		if entry.Unit == "" || len(entry.Points) != 1 || entry.Points[0].Value <= 0 {
			t.Fatalf("adapted request series mismatch: %+v", entry)
		}
		if entry.Points[0].At.IsZero() {
			t.Fatal("point timestamps must survive adaptation")
		}
	}
	if !found {
		t.Fatal("request series not adapted")
	}
}

func TestPerformanceHistoryKindMapsOperationalSeries(t *testing.T) {
	t.Parallel()

	wants := map[string]adminui.HistorySeriesKind{
		metrichistory.SeriesProcessCPU:          adminui.HistorySeriesProcessCPU,
		metrichistory.SeriesProcessMemory:       adminui.HistorySeriesProcessMemory,
		metrichistory.SeriesHostMemoryTotal:     adminui.HistorySeriesHostMemoryTotal,
		metrichistory.SeriesHostMemoryAvailable: adminui.HistorySeriesHostMemoryAvailable,
		metrichistory.SeriesStorageUse:          adminui.HistorySeriesStorageUse,
		metrichistory.SeriesStorageCap:          adminui.HistorySeriesStorageCapacity,
		metrichistory.SeriesRequests:            adminui.HistorySeriesGeneral,
	}
	for name, want := range wants {
		if got := performanceHistoryKind(name); got != want {
			t.Errorf("%s kind = %d, want %d", name, got, want)
		}
	}
}

func TestBackupStatusSourceReadsVault(t *testing.T) {
	if newBackupStatusSource(nil, "/opt/yago/data") != nil {
		t.Fatal("a nil vault must yield a nil source")
	}

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	source := newBackupStatusSource(v, "/opt/yago/data")
	status, err := source.BackupStatus(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.DataDir != "/opt/yago/data" || status.QuotaBytes != 0 || status.UsedBytes < 0 {
		t.Fatalf("status mismatch: %+v", status)
	}

	if err := v.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := source.BackupStatus(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "read storage usage") {
		t.Fatalf("a closed vault must surface the usage error, got %v", err)
	}
}

func TestGateRestartControls(t *testing.T) {
	restartCalled := false
	options := adminui.Options{
		Restart:         func() { restartCalled = true },
		RestartCrawlers: func() int { return 0 },
	}
	gateRestartControls(&options, true)
	if options.Restart == nil || options.RestartCrawlers == nil {
		t.Fatal("enabled restart must keep the controls")
	}
	options.Restart()
	if !restartCalled {
		t.Fatal("kept control must stay callable")
	}

	gateRestartControls(&options, false)
	if options.Restart != nil || options.RestartCrawlers != nil {
		t.Fatal("disabled restart must strip both controls")
	}
}

func TestLoadConfigAdminRestartToggle(t *testing.T) {
	config, err := loadNodeConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	if !config.AdminRestartEnabled {
		t.Fatal("restart controls must default to enabled")
	}

	config, err = loadNodeConfig(envWithBad(envAdminRestartEnabled, "false"))
	if err != nil {
		t.Fatalf("disabled config: %v", err)
	}
	if config.AdminRestartEnabled {
		t.Fatal("YAGO_ADMIN_RESTART_ENABLED=false must disable the controls")
	}

	if _, err := loadNodeConfig(envWithBad(envAdminRestartEnabled, "junk")); err == nil {
		t.Fatal("junk toggle must be rejected")
	}
}
