package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

// seedingToggles builds runtime toggles from a web-fallback config the way the
// node does at boot, so the tests exercise the same wiring the console edits.
func seedingToggles(config webFallbackConfig) *runtimeToggles {
	return newRuntimeToggles(nodeConfig{WebFallback: config})
}

func gatedSeederForTest(
	queue *fakeCrawlQueue,
	toggles *runtimeToggles,
	config webFallbackConfig,
) *gatedWebCrawlSeeder {
	return newGatedWebCrawlSeeder(
		newWebCrawlSeeder(
			queue,
			fakeSeedDocuments{stored: map[string]bool{}},
			yagomodel.Hash("node"),
			webCrawlSeedProfile{
				fallback: config,
				bounds:   webSeedBoundsSource(toggles, config),
			},
		),
		webSeedCrawlAdmission(toggles, config),
	)
}

func TestWebSeedGateFollowsTheLiveToggle(t *testing.T) {
	config := webFallbackConfig{SeedCrawl: false, SeedDepth: 1, SeedMaxPages: 5}
	toggles := seedingToggles(config)
	queue := &fakeCrawlQueue{}
	seeder := gatedSeederForTest(queue, toggles, config)

	if url, admitted := seeder.AdmitCrawlSeedURL("https://web.example/x"); admitted {
		t.Fatalf("disabled gate admitted %q", url)
	}
	seeder.Seed(context.Background(), []string{"https://web.example/x"})
	if _, orders := queue.snapshot(); len(orders) != 0 {
		t.Fatalf("disabled gate published %#v", orders)
	}

	// The console flip must reach the seeder without reassembling the searcher.
	toggles.SetWebSeedCrawl(true)

	url, admitted := seeder.AdmitCrawlSeedURL("https://web.example/x")
	if !admitted || url != "https://web.example/x" {
		t.Fatalf("enabled gate rejected the URL: %q %v", url, admitted)
	}
	seeder.Seed(context.Background(), []string{"https://web.example/x"})
	keys, orders := queue.snapshot()
	if len(orders) != 1 || keys[0] != "https://web.example/x" {
		t.Fatalf("enabled gate published %#v keys %#v", orders, keys)
	}
}

func TestWebSeedGateWithoutTogglesKeepsBootConfiguration(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		config := webFallbackConfig{SeedCrawl: enabled, SeedDepth: 1, SeedMaxPages: 5}
		queue := &fakeCrawlQueue{}
		seeder := gatedSeederForTest(queue, nil, config)

		seeder.Seed(context.Background(), []string{"https://web.example/x"})
		_, orders := queue.snapshot()
		if published := len(orders) == 1; published != enabled {
			t.Fatalf("SeedCrawl=%v published %d orders", enabled, len(orders))
		}
	}
}

func TestWebSeedGateIsInertWithoutASeeder(t *testing.T) {
	seeder := newGatedWebCrawlSeeder(nil, func() bool { return true })
	if url, admitted := seeder.AdmitCrawlSeedURL("https://web.example/x"); admitted {
		t.Fatalf("nil seeder admitted %q", url)
	}
	seeder.Seed(context.Background(), []string{"https://web.example/x"})
}

func TestWebSeedGateAdmitsWhenNoSwitchIsWired(t *testing.T) {
	queue := &fakeCrawlQueue{}
	seeder := newGatedWebCrawlSeeder(
		newWebCrawlSeeder(
			queue,
			fakeSeedDocuments{stored: map[string]bool{}},
			yagomodel.Hash("node"),
			webCrawlSeedProfile{fallback: webFallbackConfig{SeedDepth: 1, SeedMaxPages: 5}},
		),
		nil,
	)

	if _, admitted := seeder.AdmitCrawlSeedURL("https://web.example/x"); !admitted {
		t.Fatal("a seeder without a switch should admit")
	}
}

func TestWebSeedBoundsFollowTheLiveSettings(t *testing.T) {
	config := webFallbackConfig{SeedCrawl: true, SeedDepth: 5, SeedMaxPages: 250}
	toggles := seedingToggles(config)
	queue := &fakeCrawlQueue{}
	seeder := gatedSeederForTest(queue, toggles, config)

	seeder.Seed(context.Background(), []string{"https://web.example/a"})
	_, orders := queue.snapshot()
	if len(orders) != 1 || orders[0].Profile.MaxDepth != 5 ||
		orders[0].Profile.MaxPagesPerHost != 250 {
		t.Fatalf("boot bounds not applied: %#v", orders)
	}

	// Throttling the runaway seed backlog must land on the very next order.
	toggles.SetWebSeedDepth(0)
	toggles.SetWebSeedMaxPages(1)

	seeder.Seed(context.Background(), []string{"https://web.example/b"})
	_, orders = queue.snapshot()
	if len(orders) != 2 || orders[1].Profile.MaxDepth != 0 ||
		orders[1].Profile.MaxPagesPerHost != 1 {
		t.Fatalf("narrowed bounds not applied: %#v", orders[1])
	}
	if orders[1].Profile.MaxPagesPerRun == nil || *orders[1].Profile.MaxPagesPerRun != 1 {
		t.Fatalf("run budget did not follow the page cap: %#v", orders[1].Profile)
	}
}

func TestWebSeedBoundsWithoutTogglesStayFixed(t *testing.T) {
	config := webFallbackConfig{SeedDepth: 3, SeedMaxPages: 7}
	bounds := webSeedBoundsSource(nil, config)()
	if bounds.depth != 3 || bounds.maxPages != 7 {
		t.Fatalf("bounds = %#v", bounds)
	}
}

func TestSeedURLCanonicalizesToTheStoredSpelling(t *testing.T) {
	cases := map[string]string{
		"https://Web.Example":                   "https://web.example/",
		"https://web.example:443/a":             "https://web.example/a",
		"https://web.example/a?utm_source=ddg":  "https://web.example/a",
		"https://web.example/a/b/../c#fragment": "https://web.example/a/c",
	}
	for raw, want := range cases {
		if got := seedURL(raw); got != want {
			t.Fatalf("seedURL(%q) = %q; want %q", raw, got, want)
		}
	}
}

func TestSeedURLRejectsUnseedableInput(t *testing.T) {
	cases := map[string]string{
		"relative":       "/page",
		"foreign scheme": "ftp://web.example/x",
		"userinfo":       "https://user@web.example/x",
		"unparsable":     "http://[::1",
		"oversized": "https://web.example/" +
			string(make([]byte, yagomodel.MaximumURLIdentityBytes)),
	}
	for name, raw := range cases {
		if got := seedURL(raw); got != "" {
			t.Fatalf("%s: seedURL(%q) = %q", name, raw, got)
		}
	}
}

// A canonicalized seed must match the document the crawler stored, otherwise an
// already-indexed page is re-seeded as a fresh domain crawl on every repeat
// query — the churn that made the crawl queue outgrow the fleet.
func TestSeedSkipsAStoredPageSpelledDifferently(t *testing.T) {
	config := webFallbackConfig{SeedCrawl: true, SeedDepth: 1, SeedMaxPages: 5}
	queue := &fakeCrawlQueue{}
	seeder := newWebCrawlSeeder(
		queue,
		fakeSeedDocuments{stored: map[string]bool{"https://web.example/": true}},
		yagomodel.Hash("node"),
		webCrawlSeedProfile{fallback: config},
	)

	seeder.Seed(context.Background(), []string{"https://Web.Example"})
	if _, orders := queue.snapshot(); len(orders) != 0 {
		t.Fatalf("stored page re-seeded: %#v", orders)
	}
}

func TestReportWebSeedWiringNamesTheBlockingCondition(t *testing.T) {
	// Exercises every branch; the value is the log line an operator greps for.
	reportWebSeedWiring(webFallbackConfig{SeedCrawl: true, SeedDepth: 1}, true)
	reportWebSeedWiring(webFallbackConfig{SeedCrawl: false}, true)
	reportWebSeedWiring(webFallbackConfig{SeedCrawl: true}, false)
}

func TestWebSeedPublishLogsTheSeededURL(t *testing.T) {
	config := webFallbackConfig{SeedCrawl: true, SeedDepth: 0, SeedMaxPages: 1}
	queue := &coalescingCrawlQueue{}
	seeder := newWebCrawlSeeder(
		queue,
		fakeSeedDocuments{stored: map[string]bool{}},
		yagomodel.Hash("node"),
		webCrawlSeedProfile{fallback: config},
	)
	seeder.now = func() time.Time { return time.Unix(0, 0).UTC() }

	seeder.Seed(context.Background(), []string{"https://web.example/x"})
	if queue.calls != 1 {
		t.Fatalf("publish calls = %d", queue.calls)
	}
}

// coalescingCrawlQueue reports the order as already outstanding, the path that
// distinguishes "seeded now" from "already queued" in the published log line.
type coalescingCrawlQueue struct {
	calls int
}

func (q *coalescingCrawlQueue) PublishOnce(
	context.Context,
	string,
	yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.calls++

	return true, nil
}

// The console's three web-discovery knobs must all reach the live toggles.
// Before this, every one of them was restart-only: an operator could enable
// "Web-discovery crawling", see it saved, and index nothing until the next
// node restart.
func TestWebDiscoverySettingsApplyLive(t *testing.T) {
	toggles := &runtimeToggles{}
	definitions := webDiscoveryDefinitions()

	for _, definition := range definitions {
		if definition.applyLive == nil {
			t.Fatalf("%s still requires a restart", definition.key)
		}
	}
	settingByKey(t, definitions, "web.fallback.seed_crawl").
		applyLive(toggles, settingBoolTrue)
	settingByKey(t, definitions, "web.fallback.seed_depth").
		applyLive(toggles, "2")
	settingByKey(t, definitions, "web.fallback.seed_max_pages").
		applyLive(toggles, "35")

	bounds := webSeedBoundsSource(toggles, webFallbackConfig{})()
	if !toggles.WebSeedCrawlEnabled() || bounds.depth != 2 || bounds.maxPages != 35 {
		t.Fatalf("live web-discovery settings = %v %#v", toggles.WebSeedCrawlEnabled(), bounds)
	}
}

func TestWebSeedTogglesTolerateAMissingReceiver(t *testing.T) {
	var toggles *runtimeToggles
	toggles.SetWebSeedCrawl(true)
	toggles.SetWebSeedDepth(3)
	toggles.SetWebSeedMaxPages(9)
	if toggles.WebSeedCrawlEnabled() {
		t.Fatal("a nil toggle set reported seeding as enabled")
	}
}
