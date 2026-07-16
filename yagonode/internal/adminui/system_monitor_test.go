package adminui

import (
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

type monitorHistoryValues struct {
	cpu           float64
	processMemory float64
	hostTotal     float64
	hostAvailable float64
	storageUsed   float64
	storageQuota  float64
}

func monitorHistory(at time.Time, values monitorHistoryValues) fakeHistory {
	return fakeHistory{series: []HistorySeries{
		{
			Kind: HistorySeriesProcessCPU, Name: "Process CPU",
			Unit: "cores", Points: historyAt(at, values.cpu),
		},
		{
			Kind: HistorySeriesProcessMemory, Name: "Process memory",
			Unit: "bytes", Points: historyAt(at, values.processMemory),
		},
		{
			Kind: HistorySeriesHostMemoryTotal, Name: "Host memory total",
			Unit: "bytes", Points: historyAt(at, values.hostTotal),
		},
		{
			Kind: HistorySeriesHostMemoryAvailable, Name: "Host memory available",
			Unit: "bytes", Points: historyAt(at, values.hostAvailable),
		},
		{
			Kind: HistorySeriesStorageUse, Name: "Storage used",
			Unit: "bytes", Points: historyAt(at, values.storageUsed),
		},
		{
			Kind: HistorySeriesStorageCapacity, Name: "Storage quota",
			Unit: "bytes", Points: historyAt(at, values.storageQuota),
		},
	}}
}

func mainRegion(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `<main class="cds-main"`)
	if start < 0 {
		t.Fatal("main region missing")
	}
	end := strings.Index(body[start:], "</main>")
	if end < 0 {
		t.Fatal("main region missing")
	}

	return body[start : start+end]
}

func TestSystemMonitorBuildsExactBoundedReadings(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 16, 9, 8, 7, 0, time.UTC)
	view := buildSystemMonitorForProcessors(
		monitorHistory(at, monitorHistoryValues{
			cpu:           1.25,
			processMemory: 64 << 20,
			hostTotal:     16 << 30,
			hostAvailable: 4 << 30,
			storageUsed:   6 << 30,
			storageQuota:  4 << 30,
		}),
		8,
	)
	if !view.CPUAvailable || view.CPUValue != 1.25 || view.CPUMaximum != 8 ||
		view.CPUText != "1.25 of 8 logical CPUs" {
		t.Fatalf("CPU reading = %+v", view)
	}
	if !view.ProcessMemoryAvailable || view.ProcessMemoryValue != 64<<20 ||
		view.ProcessMemoryMaximum != 16<<30 ||
		view.ProcessMemoryText != "64.0 MiB RSS / 16.0 GiB" {
		t.Fatalf("process memory reading = %+v", view)
	}
	if !view.HostMemoryAvailable || view.HostMemoryValue != 12<<30 ||
		view.HostMemoryMaximum != 16<<30 ||
		view.HostMemoryText != "12.0 GiB / 16.0 GiB · 4.0 GiB available" {
		t.Fatalf("host memory reading = %+v", view)
	}
	if !view.StorageAvailable || !view.StorageBounded ||
		view.StorageValue != 4<<30 || view.StorageMaximum != 4<<30 ||
		view.StorageText != "6.0 GiB / 4.0 GiB" {
		t.Fatalf("storage reading = %+v", view)
	}
	if !view.Observed || view.ObservedAt != "2026-07-16T09:08:07Z" {
		t.Fatalf("observation = %+v", view)
	}
}

func TestSystemMonitorRejectsInvalidAndStaleReadings(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 16, 9, 8, 7, 0, time.UTC)
	invalid := buildSystemMonitorForProcessors(
		monitorHistory(at, monitorHistoryValues{
			cpu:           3,
			processMemory: math.Inf(1),
			hostTotal:     math.NaN(),
			hostAvailable: 4 << 30,
			storageUsed:   math.NaN(),
			storageQuota:  4 << 30,
		}),
		2,
	)
	if invalid.CPUAvailable || invalid.ProcessMemoryAvailable ||
		invalid.HostMemoryAvailable || invalid.StorageAvailable ||
		invalid.Observed {
		t.Fatalf("invalid readings became available: %+v", invalid)
	}

	stale := monitorHistory(at, monitorHistoryValues{
		cpu:           1,
		processMemory: 64 << 20,
		hostTotal:     16 << 30,
		hostAvailable: 4 << 30,
		storageUsed:   1 << 30,
		storageQuota:  4 << 30,
	})
	stale.series = append(stale.series, HistorySeries{
		Name:   "HTTP requests",
		Unit:   "req/s",
		Points: []HistoryPoint{{At: at.Add(10 * time.Second), Value: 1}},
	})
	view := buildSystemMonitorForProcessors(stale, 8)
	if view.CPUAvailable || view.ProcessMemoryAvailable || view.HostMemoryAvailable ||
		view.StorageAvailable || view.Observed {
		t.Fatalf("stale readings became current: %+v", view)
	}

	if view = buildSystemMonitorForProcessors(
		monitorHistory(at, monitorHistoryValues{
			cpu:           1,
			processMemory: 1 << 63,
			hostTotal:     1 << 63,
		}),
		0,
	); view.CPUAvailable || view.ProcessMemoryAvailable || view.HostMemoryAvailable {
		t.Fatalf("out-of-range readings became available: %+v", view)
	}
	if view = buildSystemMonitor(nil); view.Observed {
		t.Fatalf("nil source became observed: %+v", view)
	}
}

func TestSystemMonitorClampsOnlyMemoryMeters(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 16, 9, 8, 7, 0, time.UTC)
	view := buildSystemMonitorForProcessors(
		monitorHistory(at, monitorHistoryValues{
			cpu:           1,
			processMemory: 10 << 30,
			hostTotal:     8 << 30,
			hostAvailable: 9 << 30,
		}),
		8,
	)
	if !view.ProcessMemoryAvailable || view.ProcessMemoryValue != 8<<30 ||
		view.ProcessMemoryMaximum != 8<<30 ||
		view.ProcessMemoryText != "10.0 GiB RSS / 8.0 GiB" {
		t.Fatalf("process memory clamp = %+v", view)
	}
	if view.HostMemoryAvailable {
		t.Fatalf("host memory underflow became available: %+v", view)
	}
}

func TestSystemMonitorRendersLiveShelfAndFragment(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 16, 9, 8, 7, 0, time.UTC)
	console := New(Options{PerformanceHistory: monitorHistory(at, monitorHistoryValues{
		cpu:           1.25,
		processMemory: 64 << 20,
		hostTotal:     16 << 30,
		hostAvailable: 4 << 30,
		storageUsed:   2 << 30,
		storageQuota:  8 << 30,
	})})
	page := do(t, console, "/admin/overview")
	for _, want := range []string{
		`class="cds-shelf"`,
		`id="system-monitor"`,
		`hx-get="/admin/system-monitor"`,
		`hx-trigger="every 10s"`,
		`class="cds-system-monitor__meter" min="0"`,
		`64.0 MiB RSS / 16.0 GiB`,
		`12.0 GiB / 16.0 GiB · 4.0 GiB available`,
		`aria-label="Process resident memory usage"`,
		`aria-label="Host memory usage"`,
		`2.0 GiB / 8.0 GiB`,
		`datetime="2026-07-16T09:08:07Z"`,
		mustAdminAssetReferences(assetFS)["photon_shell.css"],
	} {
		if !strings.Contains(page.body, want) {
			t.Fatalf("shelf missing %q", want)
		}
	}
	if strings.Contains(page.body, ` style=`) {
		t.Fatal("system monitor uses an inline style")
	}

	fragment := do(t, console, systemMonitorPath)
	if fragment.status != http.StatusOK || !strings.Contains(fragment.body, `id="system-monitor"`) {
		t.Fatalf("fragment = %d %q", fragment.status, fragment.body)
	}
	if strings.Contains(fragment.body, "<header") || strings.Contains(fragment.body, "<nav") ||
		strings.Contains(fragment.body, "<aside") {
		t.Fatalf("fragment contains full chrome: %s", fragment.body)
	}
}

func TestSystemMonitorRendersUnavailableAndUnlimitedStates(t *testing.T) {
	t.Parallel()

	fragment := do(t, New(Options{}), systemMonitorPath)
	if strings.Count(fragment.body, ">Unavailable<") != 4 ||
		!strings.Contains(fragment.body, "Sample: Unavailable") ||
		strings.Contains(fragment.body, "<meter") {
		t.Fatalf("unknown monitor state = %s", fragment.body)
	}

	at := time.Date(2026, 7, 16, 9, 8, 7, 0, time.UTC)
	unlimited := do(
		t,
		New(Options{PerformanceHistory: monitorHistory(at, monitorHistoryValues{
			hostTotal:     8 << 30,
			hostAvailable: 4 << 30,
			storageUsed:   2 << 30,
		})}),
		systemMonitorPath,
	)
	if !strings.Contains(unlimited.body, "2.0 GiB / unlimited") ||
		strings.Count(unlimited.body, "<meter") != 3 {
		t.Fatalf("unlimited storage state = %s", unlimited.body)
	}
}

func TestPhotonShelfKeepsFlatNavigationOrder(t *testing.T) {
	t.Parallel()

	body := do(t, New(Options{}), "/admin/overview").body
	start := strings.Index(body, `<nav class="cds-nav"`)
	if start < 0 {
		t.Fatal("navigation markup missing")
	}
	end := strings.Index(body[start:], "</nav>")
	if end < 0 {
		t.Fatal("navigation markup missing")
	}
	navigation := body[start : start+end]
	if strings.Contains(navigation, "<details") || strings.Contains(navigation, "<summary") {
		t.Fatal("navigation must stay a flat list")
	}
	previous := -1
	for _, item := range navItems {
		position := strings.Index(navigation, `href="`+item.Path+`"`)
		if position <= previous {
			t.Fatalf("navigation order changed at %s", item.Path)
		}
		previous = position
	}
}

func TestPhotonShelfAndTablePrimitivesAreStructural(t *testing.T) {
	t.Parallel()

	shell, err := assetFS.ReadFile("assets/photon_shell.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`.cds-shelf .cds-nav__list li {`,
		`border-bottom: 1px solid var(--ph-shadow);`,
		`.cds-system-monitor__meter`,
		`grid-template-columns: repeat(2, minmax(0, 1fr));`,
		`@media (max-width: 20rem)`,
		`@media (forced-colors: active)`,
	} {
		if !strings.Contains(string(shell), want) {
			t.Fatalf("Photon shelf CSS missing %q", want)
		}
	}

	stylesheet, err := assetFS.ReadFile("assets/photon.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(stylesheet)
	for _, want := range []string{
		`border-color: var(--ph-dark) var(--ph-light) var(--ph-light) var(--ph-dark);`,
		`.cds-table:has(> thead) tbody > tr > * {`,
		`.cds-table:not(:has(> thead)) tbody > tr > * {`,
		`.cds-table thead th:last-child { border-right: 0; }`,
		`.cds-table tbody tr:hover > *`,
		`.cds-table tbody tr[aria-selected="true"] > *`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("Photon table CSS missing %q", want)
		}
	}
	perCellHeaderShadow := `.cds-table thead th { background: var(--cds-layer-01);` +
		` box-shadow: var(--ph-raise1);`
	if strings.Contains(css, perCellHeaderShadow) {
		t.Fatal("table headers retain per-cell top shadows")
	}
}
