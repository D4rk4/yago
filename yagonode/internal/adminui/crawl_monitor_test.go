package adminui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

var errStubControl = errors.New("stub control failure")

type fakeMonitor struct {
	snap CrawlMonitor
}

func (f fakeMonitor) Monitor(context.Context) CrawlMonitor {
	return f.snap
}

type fakeControl struct {
	got CrawlControlRequest
	err error
}

func (f *fakeControl) Control(_ context.Context, req CrawlControlRequest) error {
	f.got = req

	return f.err
}

func sampleMonitor() CrawlMonitor {
	return CrawlMonitor{
		Runs: []CrawlRunView{{
			RunID:          "run-abc",
			Profile:        "news-crawl",
			State:          "running",
			Fetched:        12,
			Indexed:        9,
			Failed:         1,
			Pending:        4,
			Runtime:        "2m0s",
			PagesPerMinute: 30,
			RateKnown:      true,
		}},
		Totals: CrawlTotals{
			Fetched:      12,
			Indexed:      9,
			Failed:       1,
			RobotsDenied: 3,
			Duplicates:   5,
		},
		QueueAvailable: true,
		QueuePending:   7,
		QueueLeased:    2,
	}
}

func TestConsoleCrawlRendersMonitor(t *testing.T) {
	t.Parallel()

	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: sampleMonitor()}})
	got := do(t, console, "/admin/crawl")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{
		"Crawl monitor", "news-crawl", "cds-tag--info", "7 pending, 2 leased",
		"Crawl results and rejections", "Robots-denied", "currently retained in this monitor",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("crawl page missing %q", want)
		}
	}
}

func TestConsoleCrawlMonitorRendersResultTotals(t *testing.T) {
	t.Parallel()

	monitor := CrawlMonitor{Totals: CrawlTotals{Indexed: 42, RobotsDenied: 7}}
	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: monitor}})
	got := do(t, console, "/admin/crawl/monitor")
	for _, want := range []string{"Fetched", "Indexed", "Failed", "Duplicates", ">42<", ">7<"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("results rollup missing %q", want)
		}
	}
}

func TestConsoleCrawlMonitorShowsUnavailableQueue(t *testing.T) {
	t.Parallel()

	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: CrawlMonitor{}}})
	got := do(t, console, "/admin/crawl/monitor")
	if !strings.Contains(got.body, "order queue: Unavailable") {
		t.Fatal("failed queue probe should render unavailable")
	}
	if strings.Contains(got.body, "0 pending, 0 leased") {
		t.Fatal("failed queue probe rendered fabricated zero depths")
	}
}

func TestConsoleCrawlMonitorLabelsFailureRatePopulation(t *testing.T) {
	t.Parallel()

	monitor := CrawlMonitor{Totals: CrawlTotals{Failed: 100}}
	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: monitor}})
	got := do(t, console, "/admin/crawl/monitor")
	for _, want := range []string{"Failure rate", "100%", "failed / (fetched + failed)"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("failure-rate tile missing %q", want)
		}
	}
}

func TestConsoleCrawlMonitorLabelsLinkRedundancyPopulation(t *testing.T) {
	t.Parallel()

	monitor := CrawlMonitor{
		Totals: CrawlTotals{Fetched: 100, Failed: 5, RobotsDenied: 5, Duplicates: 10},
	}
	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: monitor}})
	got := do(t, console, "/admin/crawl/monitor")
	for _, want := range []string{
		"Link redundancy",
		"duplicates / (duplicates + fetched + failed + robots-denied)",
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("link-redundancy tile missing %q", want)
		}
	}
}

func TestConsoleCrawlMonitorPartial(t *testing.T) {
	t.Parallel()

	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: sampleMonitor()}})
	got := do(t, console, "/admin/crawl/monitor")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if !strings.Contains(got.body, "news-crawl") {
		t.Fatal("monitor partial missing run row")
	}
	if strings.Contains(got.body, "<html") {
		t.Fatal("monitor partial should render the fragment, not the full layout")
	}
}

func TestConsoleCrawlMonitorPartialNotFoundWithoutSource(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Crawl: &fakeCrawl{}}), "/admin/crawl/monitor")
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404 without a monitor source", got.status)
	}
}

func TestConsoleCrawlWithoutMonitorRendersFormOnly(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Crawl: &fakeCrawl{}}), "/admin/crawl")
	if strings.Contains(got.body, "Crawl monitor") {
		t.Fatal("crawl page rendered a monitor without a monitor source")
	}
}

func TestConsoleCrawlMonitorEmptyRuns(t *testing.T) {
	t.Parallel()

	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: CrawlMonitor{}}})
	got := do(t, console, "/admin/crawl/monitor")
	if !strings.Contains(got.body, "No crawl runs observed yet.") {
		t.Fatal("empty monitor missing the empty state")
	}
}

func TestConsoleCrawlMonitorRendersControlButtons(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Crawl:   &fakeCrawl{},
		Monitor: fakeMonitor{snap: sampleMonitor()},
		Control: &fakeControl{},
	})
	got := do(t, console, "/admin/crawl/monitor")
	for _, want := range []string{
		`action="/admin/crawl/control"`, `name="runId" value="run-abc"`,
		`name="action" value="pause"`, `name="action" value="resume"`,
		`name="action" value="cancel"`, `hx-confirm=`,
		`name="action" value="set_rate"`, `name="ppm"`, `Apply rate`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("monitor missing control %q", want)
		}
	}
}

func TestConsoleCrawlMonitorLaysControlsOnOneLine(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Crawl:   &fakeCrawl{},
		Monitor: fakeMonitor{snap: sampleMonitor()},
		Control: &fakeControl{},
	})
	got := do(t, console, "/admin/crawl/monitor")
	for _, want := range []string{
		`class="cds-control-row cds-control-row--crawl-actions"`,
		`class="cds-crawl-actions-cell"`,
		`class="cds-input cds-input--rate"`,
		`for="crawl-rate-run-abc"`,
		`id="crawl-rate-run-abc"`,
		`value="30"`,
		`title="0 means unlimited"`,
		`aria-label="Pages per minute for news-crawl; zero means unlimited"`,
		`class="cds-scroll-x" role="region" aria-label="Crawl tasks" tabindex="0"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("monitor controls missing %q", want)
		}
	}

	css := do(t, console, "/admin/assets/carbon.css")
	for _, want := range []string{
		".cds-control-row {\n  display: flex;\n  flex-wrap: wrap;",
		".cds-control-row--crawl-actions {\n  flex-wrap: nowrap;\n  min-width: max-content;\n  gap: var(--cds-spacing-03);\n}",
		".cds-crawl-actions-cell { width: 1%; white-space: nowrap; }",
		".cds-control-row--crawl-actions .cds-inline-form { flex: 0 0 auto; }",
		".cds-control-row--crawl-actions .cds-btn {\n  min-height: 2rem;\n  padding: 0 var(--cds-spacing-03);\n}",
		".cds-control-row--crawl-actions .cds-input--rate {\n  width: 4rem;\n  height: 2rem;\n  padding: 0 var(--cds-spacing-03);\n}",
	} {
		if !strings.Contains(css.body, want) {
			t.Fatalf("crawler control CSS missing %q", want)
		}
	}
}

func TestConsoleCrawlMonitorPrefillsCurrentRate(t *testing.T) {
	t.Parallel()

	monitor := CrawlMonitor{Runs: []CrawlRunView{{
		RunID:          "run-abc",
		Profile:        "news-crawl",
		State:          "running",
		PagesPerMinute: 45,
		RateKnown:      true,
	}}}
	console := New(Options{
		Crawl:   &fakeCrawl{},
		Monitor: fakeMonitor{snap: monitor},
		Control: &fakeControl{},
	})
	got := do(t, console, "/admin/crawl/monitor")
	if !strings.Contains(got.body, `value="45"`) {
		t.Fatal("running run with an applied rate should pre-fill the rate field")
	}
}

func TestConsoleCrawlMonitorPrefillsUnlimitedRate(t *testing.T) {
	t.Parallel()

	monitor := CrawlMonitor{Runs: []CrawlRunView{{
		RunID:          "run-zero",
		Profile:        "wide-crawl",
		State:          "running",
		PagesPerMinute: 0,
		RateKnown:      true,
	}}}
	console := New(Options{Monitor: fakeMonitor{snap: monitor}, Control: &fakeControl{}})
	got := do(t, console, "/admin/crawl/monitor")
	if !strings.Contains(got.body, `id="crawl-rate-run-zero"`) ||
		!strings.Contains(got.body, `value="0"`) {
		t.Fatal("known unlimited rate should pre-fill the rate field with zero")
	}
}

func TestParsePagesPerMinute(t *testing.T) {
	t.Parallel()

	valid := map[string]uint32{
		"45":  45,
		"0":   0,
		"\t7": 7,
	}
	for raw, want := range valid {
		got, err := parsePagesPerMinute(raw)
		if err != nil || got != want {
			t.Fatalf("parsePagesPerMinute(%q) = %d, %v; want %d, nil", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "abc", "-5", "999999999999"} {
		if got, err := parsePagesPerMinute(raw); err == nil {
			t.Fatalf("parsePagesPerMinute(%q) = %d, nil; want an error", raw, got)
		}
	}
}

func TestConsoleCrawlControlSetRateParsesPagesPerMinute(t *testing.T) {
	t.Parallel()

	control := &fakeControl{}
	got := doPost(t, New(Options{Control: control}), "/admin/crawl/control", url.Values{
		"runId":  {"run-abc"},
		"action": {"set_rate"},
		"ppm":    {"90"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", got.status)
	}
	if control.got.Action != "set_rate" || control.got.PagesPerMinute != 90 {
		t.Fatalf("control received %+v, want set_rate/90", control.got)
	}
}

func TestConsoleCrawlControlRejectsInvalidRateWithoutLiftingThrottle(t *testing.T) {
	t.Parallel()

	control := &fakeControl{}
	got := doPost(t, New(Options{Control: control}), "/admin/crawl/control", url.Values{
		"runId":  {"run-abc"},
		"action": {"set_rate"},
		"ppm":    {"not-a-rate"},
	})
	if got.status != http.StatusBadRequest ||
		!strings.Contains(got.body, "Invalid pages/minute rate") {
		t.Fatalf("response = %d %q, want a clear 400", got.status, got.body)
	}
	if control.got.Action != "" {
		t.Fatalf("invalid rate reached control: %+v", control.got)
	}
}

func TestConsoleCrawlControlHtmxRefreshesMonitor(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Monitor: fakeMonitor{snap: sampleMonitor()},
		Control: &fakeControl{},
	})
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/admin/crawl/control",
		strings.NewReader(url.Values{"runId": {"run-abc"}, "action": {"cancel"}}.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	console.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 for an htmx control", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `id="crawl-monitor"`) {
		t.Fatal("htmx control response is not the refreshed monitor partial")
	}
}

func TestConsoleCrawlMonitorHidesControlsWithoutSource(t *testing.T) {
	t.Parallel()

	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: sampleMonitor()}})
	got := do(t, console, "/admin/crawl/monitor")
	if strings.Contains(got.body, `name="action" value="pause"`) {
		t.Fatal("control buttons rendered without a control source")
	}
}

func TestConsoleCrawlControlDispatches(t *testing.T) {
	t.Parallel()

	control := &fakeControl{}
	got := doPost(t, New(Options{Control: control}), "/admin/crawl/control", url.Values{
		"runId":  {"run-abc"},
		"action": {"pause"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", got.status)
	}
	if control.got.RunID != "run-abc" || control.got.Action != "pause" {
		t.Fatalf("control received %+v, want run-abc/pause", control.got)
	}
}

func TestConsoleCrawlControlSurvivesSourceError(t *testing.T) {
	t.Parallel()

	control := &fakeControl{err: errStubControl}
	got := doPost(t, New(Options{Control: control}), "/admin/crawl/control", url.Values{
		"runId":  {"run-abc"},
		"action": {"resume"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("status %d, want 303 even when the control source errs", got.status)
	}
}

func TestConsoleCrawlControlNotFoundWithoutSource(t *testing.T) {
	t.Parallel()

	got := doPost(t, New(Options{}), "/admin/crawl/control", url.Values{
		"runId":  {"run-abc"},
		"action": {"pause"},
	})
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want 404 without a control source", got.status)
	}
}

func TestCrawlStateTag(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"running":   "info",
		"cancelled": "warn",
		"finished":  "",
		"unknown":   "",
	}
	for state, want := range cases {
		if got := crawlStateTag(state); got != want {
			t.Fatalf("crawlStateTag(%q) = %q, want %q", state, got, want)
		}
	}
}

func TestConsoleCrawlMonitorOffersRestartForFinishedRuns(t *testing.T) {
	t.Parallel()

	console := New(Options{
		Monitor: fakeMonitor{snap: CrawlMonitor{Runs: []CrawlRunView{
			{RunID: "run-done", Profile: "HandleAAAAAA", State: "finished"},
			{RunID: "run-live", Profile: "HandleBBBBBB", State: "running"},
		}}},
		Control: &fakeControl{},
	})
	got := do(t, console, "/admin/crawl/monitor")
	if !strings.Contains(got.body, `name="action" value="restart"`) {
		t.Fatal("finished run should offer a restart action")
	}
	if !strings.Contains(got.body, `value="run-done"`) {
		t.Fatal("restart form should carry the finished run id")
	}
}

func TestConsoleCrawlControlRestartDispatches(t *testing.T) {
	t.Parallel()

	control := &fakeControl{}
	got := doPost(t, New(Options{Control: control}), "/admin/crawl/control", url.Values{
		"runId":  {"run-done"},
		"action": {"restart"},
	})
	if got.status != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", got.status)
	}
	if control.got.Action != "restart" || control.got.RunID != "run-done" {
		t.Fatalf("control received %+v, want restart/run-done", control.got)
	}
}
