package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

// The seeded page cap has two authorities: the operator's per-order cap and the
// crawler's own per-run maximum. Only the lower of the two may reach the order,
// and the existing coverage exercises the cap that is already low enough. This
// pins the other direction: when the crawler is the stricter authority, the
// order must carry the crawler's number, not the operator's, or one
// web-discovery query enqueues a run the crawler has already refused to accept
// at that size.
func TestAutomaticDiscoveryPageLimitYieldsToAStricterCrawler(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		configured     int
		crawlerMaximum int
		want           int
	}{
		{name: "crawler is stricter", configured: 20, crawlerMaximum: 5, want: 5},
		{name: "crawler is one page stricter", configured: 20, crawlerMaximum: 19, want: 19},
		{name: "authorities agree", configured: 20, crawlerMaximum: 20, want: 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := automaticDiscoveryPageLimit(test.configured, test.crawlerMaximum)
			if got != test.want {
				t.Fatalf(
					"page limit for configured %d and crawler maximum %d = %d, want %d",
					test.configured,
					test.crawlerMaximum,
					got,
					test.want,
				)
			}
		})
	}
}

// The published order is where the clamp has to be observable: an operator who
// raised the seed page cap above what the crawler will run must still see the
// crawler's number on the wire.
func TestWebCrawlSeederPublishesTheCrawlerPageMaximum(t *testing.T) {
	queue := &fakeCrawlQueue{}
	seeder := newWebCrawlSeeder(
		queue,
		fakeSeedDocuments{},
		yagomodel.Hash("node"),
		webCrawlSeedProfile{
			fallback:       webFallbackConfig{SeedDepth: 1, SeedMaxPages: 20},
			maxPagesPerRun: func() int { return 5 },
		},
	)

	seeder.Seed(t.Context(), []string{"https://fresh.example/page"})

	_, orders := queue.snapshot()
	if len(orders) != 1 {
		t.Fatalf("orders = %d, want 1", len(orders))
	}
	maximum := orders[0].Profile.MaxPagesPerRun
	if maximum == nil {
		t.Fatal("published order carries no max pages per run")
	}
	if *maximum != 5 {
		t.Fatalf("published max pages per run = %d, want the crawler maximum 5", *maximum)
	}
	if orders[0].Profile.MaxPagesPerHost != 20 {
		t.Fatalf(
			"per-host cap = %d, want the operator's 20 left alone",
			orders[0].Profile.MaxPagesPerHost,
		)
	}
}
