package adminui

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestConsoleRestartConfirmAndAction(t *testing.T) {
	t.Parallel()

	restarts := 0
	console := New(Options{Restart: func() { restarts++ }})

	got := do(t, console, "/admin/restart")
	if got.status != http.StatusOK {
		t.Fatalf("confirm page status %d", got.status)
	}
	for _, want := range []string{"Restart node", `action="/admin/restart"`, "Cancel"} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("confirm page missing %q", want)
		}
	}
	if restarts != 0 {
		t.Fatal("viewing the confirmation must not restart")
	}

	posted := doPost(t, console, "/admin/restart", url.Values{})
	if posted.status != http.StatusOK {
		t.Fatalf("action status %d", posted.status)
	}
	for _, want := range []string{"Restarting", `http-equiv="refresh"`, "/admin/overview"} {
		if !strings.Contains(posted.body, want) {
			t.Fatalf("restarting page missing %q", want)
		}
	}
	if restarts != 1 {
		t.Fatalf("restart triggered %d times, want 1", restarts)
	}
}

func TestConsoleRestartUnavailableWithoutTrigger(t *testing.T) {
	t.Parallel()

	console := New(Options{})
	got := do(t, console, "/admin/restart")
	if got.status != http.StatusOK || !strings.Contains(got.body, "not wired") {
		t.Fatalf("unavailable page = %d %.80q", got.status, got.body)
	}
	posted := doPost(t, console, "/admin/restart", url.Values{})
	if posted.status != http.StatusNotFound {
		t.Fatalf("action without trigger = %d, want 404", posted.status)
	}
}

func TestHeaderDropsSecurityAndRestartLinks(t *testing.T) {
	t.Parallel()

	console := New(Options{Config: fakeConfig{view: ConfigView{}}, PublicAddr: ":8090"})
	got := do(t, console, "/admin/overview")

	header := got.body[strings.Index(got.body, "<header"):strings.Index(got.body, "</header>")]
	if strings.Contains(header, "/admin/security") {
		t.Fatal("Security should not be duplicated in the header; it lives in the nav")
	}
	if strings.Contains(header, "/admin/restart") {
		t.Fatal("Restart should live in the nav, not the header")
	}
	if !strings.Contains(header, `aria-label="Public search"`) ||
		!strings.Contains(header, `href="#ic-globe"`) {
		t.Fatalf("public search icon missing from header: %s", header)
	}
}

func TestRestartIsANavItem(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{}), "/admin/overview")
	if !strings.Contains(got.body, `href="/admin/restart"`) ||
		!strings.Contains(got.body, `cds-nav__label">Restart</span>`) ||
		!strings.Contains(got.body, `href="#ic-restart"`) {
		t.Fatal("Restart nav item with its icon is missing")
	}
}

func TestRestartPageShowsCrawlerControlWhenWired(t *testing.T) {
	t.Parallel()

	console := New(Options{Restart: func() {}, RestartCrawlers: func() int { return 0 }})
	got := do(t, console, "/admin/restart")
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	for _, want := range []string{"Restart node", "Restart crawlers", `value="crawler"`} {
		if !strings.Contains(got.body, want) {
			t.Fatalf("restart page missing %q", want)
		}
	}
}

func TestRestartPageHidesCrawlerControlWhenUnwired(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{Restart: func() {}}), "/admin/restart")
	if strings.Contains(got.body, "Restart crawlers") {
		t.Fatal("crawler-restart control rendered without a source")
	}
}

func TestRestartPageAvailableWithOnlyCrawlerRestart(t *testing.T) {
	t.Parallel()

	got := do(t, New(Options{RestartCrawlers: func() int { return 0 }}), "/admin/restart")
	if got.status != http.StatusOK || !strings.Contains(got.body, "Restart crawlers") {
		t.Fatalf("crawler-only restart page = %d %.60q", got.status, got.body)
	}
}

func TestCrawlerRestartSignalsAndReportsCount(t *testing.T) {
	t.Parallel()

	calls := 0
	console := New(Options{Restart: func() {}, RestartCrawlers: func() int { calls++; return 3 }})
	got := doPost(t, console, "/admin/restart", url.Values{"target": {"crawler"}})
	if got.status != http.StatusOK {
		t.Fatalf("status %d", got.status)
	}
	if calls != 1 {
		t.Fatalf("restartCrawlers called %d times, want 1", calls)
	}
	if !strings.Contains(got.body, "Signalled 3 crawlers to restart.") {
		t.Fatalf("notice missing plural count: %s", got.body)
	}
}

func TestCrawlerRestartReportsSingularAndNone(t *testing.T) {
	t.Parallel()

	one := New(Options{RestartCrawlers: func() int { return 1 }})
	got := doPost(t, one, "/admin/restart", url.Values{"target": {"crawler"}})
	if !strings.Contains(got.body, "Signalled 1 crawler to restart.") {
		t.Fatalf("singular notice missing: %s", got.body)
	}

	none := New(Options{RestartCrawlers: func() int { return 0 }})
	got = doPost(t, none, "/admin/restart", url.Values{"target": {"crawler"}})
	if !strings.Contains(got.body, "No crawlers are connected.") {
		t.Fatalf("empty notice missing: %s", got.body)
	}
}

func TestCrawlerRestartTargetWithoutSourceIsNotFound(t *testing.T) {
	t.Parallel()

	console := New(Options{Restart: func() {}})
	got := doPost(t, console, "/admin/restart", url.Values{"target": {"crawler"}})
	if got.status != http.StatusNotFound {
		t.Fatalf("crawler restart without source = %d, want 404", got.status)
	}
}
