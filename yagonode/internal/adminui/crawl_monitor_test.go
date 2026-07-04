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
			RunID:   "run-abc",
			Profile: "news-crawl",
			State:   "running",
			Fetched: 12,
			Indexed: 9,
			Failed:  1,
			Pending: 4,
			Runtime: "2m0s",
		}},
		Totals: CrawlTotals{
			Fetched:      12,
			Indexed:      9,
			Failed:       1,
			RobotsDenied: 3,
			Duplicates:   5,
		},
		QueuePending: 7,
		QueueLeased:  2,
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
		"Crawl results and rejections", "Robots-denied",
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

func TestParsePagesPerMinute(t *testing.T) {
	t.Parallel()

	cases := map[string]uint32{
		"45": 45, "0": 0, "": 0, "abc": 0, "-5": 0, "999999999999": 0,
	}
	for raw, want := range cases {
		if got := parsePagesPerMinute(raw); got != want {
			t.Fatalf("parsePagesPerMinute(%q) = %d, want %d", raw, got, want)
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
