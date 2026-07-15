package adminui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func crawlRunFixtures() []CrawlRunView {
	const total = 45

	runs := make([]CrawlRunView, total)
	for position := range runs {
		profileKind := "manual"
		if position%2 == 0 {
			profileKind = "scheduled"
		}
		runs[position] = CrawlRunView{
			RunID:   fmt.Sprintf("run-%03d", position),
			Profile: fmt.Sprintf("%s-%03d", profileKind, position),
			State:   "running",
		}
	}

	return runs
}

func TestBuildCrawlRunPaginationBoundsEveryPage(t *testing.T) {
	t.Parallel()

	runs := crawlRunFixtures()
	first := buildCrawlRunPagination(runs, "")
	if first.Total != 45 || first.Pages != 3 || first.Page != 1 ||
		len(first.Runs) != crawlRunsPerPage || first.Start != 1 || first.End != 20 ||
		first.HasPrev || !first.HasNext {
		t.Fatalf("first page = %+v", first)
	}
	if first.Runs[0].RunID != "run-000" || first.Runs[19].RunID != "run-019" {
		t.Fatalf("first page bounds = %q through %q", first.Runs[0].RunID, first.Runs[19].RunID)
	}

	middle := buildCrawlRunPagination(runs, "2")
	if middle.Page != 2 || len(middle.Runs) != crawlRunsPerPage ||
		middle.Start != 21 || middle.End != 40 || !middle.HasPrev || !middle.HasNext {
		t.Fatalf("middle page = %+v", middle)
	}
	if middle.Runs[0].RunID != "run-020" || middle.Runs[19].RunID != "run-039" {
		t.Fatalf("middle page bounds = %q through %q", middle.Runs[0].RunID, middle.Runs[19].RunID)
	}

	last := buildCrawlRunPagination(runs, "3")
	if last.Page != 3 || len(last.Runs) != 5 || last.Start != 41 || last.End != 45 ||
		!last.HasPrev || last.HasNext {
		t.Fatalf("last page = %+v", last)
	}
	if last.Runs[0].RunID != "run-040" || last.Runs[4].RunID != "run-044" {
		t.Fatalf("last page bounds = %q through %q", last.Runs[0].RunID, last.Runs[4].RunID)
	}
}

func TestBuildCrawlRunPaginationClampsPageAndBuildsNavigation(t *testing.T) {
	t.Parallel()

	runs := crawlRunFixtures()
	clamped := buildCrawlRunPagination(runs, "99")
	if clamped.Page != 3 || clamped.RefreshURL != "/admin/crawl/monitor?cpage=3" {
		t.Fatalf("clamped page = %+v", clamped)
	}
	if clamped.PrevURL != "/admin/crawl?cpage=2#crawl-monitor" || clamped.NextURL != "" {
		t.Fatalf("clamped navigation = %+v", clamped)
	}

	invalid := buildCrawlRunPagination(runs, "not-a-page")
	if invalid.Page != 1 || invalid.RefreshURL != "/admin/crawl/monitor" ||
		invalid.PrevURL != "" || invalid.NextURL != "/admin/crawl?cpage=2#crawl-monitor" {
		t.Fatalf("invalid navigation = %+v", invalid)
	}

	empty := buildCrawlRunPagination(nil, "4")
	if empty.Page != 1 || empty.Pages != 1 || empty.Total != 0 || len(empty.Runs) != 0 ||
		empty.Start != 0 || empty.End != 0 || empty.HasPrev || empty.HasNext {
		t.Fatalf("empty page = %+v", empty)
	}
}

func TestConsoleCrawlRunPaginationMatchesNetworkPager(t *testing.T) {
	t.Parallel()

	monitor := CrawlMonitor{Runs: crawlRunFixtures()}
	console := New(Options{Crawl: &fakeCrawl{}, Monitor: fakeMonitor{snap: monitor}})
	first := do(t, console, "/admin/crawl")
	for _, expected := range []string{
		"scheduled-000", "manual-019", "Page 1 of 3",
		"tasks 1–20 of 45", "Next ›", `aria-label="Crawl task pages"`,
	} {
		if !strings.Contains(first.body, expected) {
			t.Fatalf("first page missing %q", expected)
		}
	}
	if strings.Contains(first.body, "scheduled-020") ||
		strings.Contains(first.body, "‹ Previous") {
		t.Fatal("first page rendered a later task or a previous link")
	}

	middle := do(t, console, "/admin/crawl?cpage=2")
	for _, expected := range []string{
		"scheduled-020", "manual-039", "Page 2 of 3",
		"tasks 21–40 of 45", "‹ Previous", "Next ›",
		`hx-get="/admin/crawl/monitor?cpage=2"`,
	} {
		if !strings.Contains(middle.body, expected) {
			t.Fatalf("middle page missing %q", expected)
		}
	}
	if strings.Contains(middle.body, "manual-019") ||
		strings.Contains(middle.body, "scheduled-040") {
		t.Fatal("middle page rendered a task outside its twenty-row window")
	}

	last := do(t, console, "/admin/crawl/monitor?cpage=3")
	if !strings.Contains(last.body, "scheduled-040") ||
		!strings.Contains(last.body, "tasks 41–45 of 45") ||
		!strings.Contains(last.body, "‹ Previous") || strings.Contains(last.body, "Next ›") {
		t.Fatal("last page did not render the unified tail and previous-only navigation")
	}
}

func TestConsoleCrawlControlKeepsCurrentRunPage(t *testing.T) {
	t.Parallel()

	monitor := CrawlMonitor{Runs: crawlRunFixtures()}
	control := &fakeControl{}
	console := New(Options{Monitor: fakeMonitor{snap: monitor}, Control: control})
	plain := doPost(t, console, "/admin/crawl/control", url.Values{
		"runId": {"run-020"}, "action": {"pause"}, "cpage": {"2"},
	})
	if plain.status != http.StatusSeeOther ||
		plain.header.Get("Location") != "/admin/crawl?cpage=2#crawl-monitor" {
		t.Fatalf("plain control redirect = %d %q", plain.status, plain.header.Get("Location"))
	}

	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/admin/crawl/control",
		strings.NewReader(url.Values{
			"runId": {"run-020"}, "action": {"pause"}, "cpage": {"2"},
		}.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	recorder := httptest.NewRecorder()
	console.ServeHTTP(recorder, req)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "manual-021") ||
		!strings.Contains(body, `name="cpage" value="2"`) ||
		strings.Contains(body, "manual-019") {
		t.Fatalf("htmx control did not retain page two: status %d", recorder.Code)
	}
}
