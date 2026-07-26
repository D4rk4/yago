package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
)

// seedDueAutomaticDiscoveryURL records a web-discovery seed profile exactly as the
// dispatch registry does for a published automatic-discovery order, then makes one
// of its URLs due for recrawl. legacyBudget reproduces a profile persisted before
// seeded orders carried an explicit whole-run cap: its handle omits that field, so
// old records keep resolving under the same handle after an upgrade.
func seedDueAutomaticDiscoveryURL(
	t *testing.T,
	frontier *recrawlfrontier.Frontier,
	url string,
	legacyBudget bool,
) yagocrawlcontract.CrawlProfile {
	t.Helper()
	template := yagocrawlcontract.CrawlProfile{
		Name:            webSeedProfileName,
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        5,
		MaxPagesPerHost: 250,
		RecrawlIfOlder:  time.Hour,
	}
	if !legacyBudget {
		budget := 250
		template.MaxPagesPerRun = &budget
	}
	profile := yagocrawlcontract.NewCrawlProfile(template)
	ctx := context.Background()
	if err := frontier.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	if err := frontier.RecordFetch(ctx, url, profile.Handle, sweepBase); err != nil {
		t.Fatalf("record fetch: %v", err)
	}

	return profile
}

func sweepOneDueURL(
	t *testing.T,
	frontier *recrawlfrontier.Frontier,
) yagocrawlcontract.CrawlOrder {
	t.Helper()
	publisher := &capturingPublisher{}
	sweeper := recrawlSweeper{
		frontier:  frontier,
		publisher: publisher,
		initiator: yagomodel.Hash("node"),
		mint:      func() []byte { return []byte("provenance") },
		now:       func() time.Time { return sweepBase.Add(2 * time.Hour) },
		batch:     8,
	}
	sweeper.sweepOnce(context.Background())
	orders := publisher.snapshot()
	if len(orders) != 1 {
		t.Fatalf("re-dispatched orders = %d, want 1", len(orders))
	}

	return orders[0]
}

// A recrawl of an automatic-discovery URL must be re-dispatched as automatic
// discovery. The whole-run page budget is clamped to the per-host cap only for
// that priority, so losing it turned a bounded web-discovery task into a run
// carrying the operator default of DefaultMaxPagesPerRun pages.
func TestRecrawlSweepPreservesAutomaticDiscoveryPriority(t *testing.T) {
	frontier := openTestFrontier(t)
	seedDueAutomaticDiscoveryURL(t, frontier, "https://seed.example/page", false)

	order := sweepOneDueURL(t, frontier)

	if order.Priority != yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery {
		t.Fatalf("re-dispatched priority = %q, want automatic discovery", order.Priority)
	}
}

// The regression this reproduces: a seed profile persisted before seeded orders
// carried an explicit whole-run cap has no MaxPagesPerRun, so a re-dispatch that
// also drops the priority resolves the budget to the crawler's 50,000-page
// fallback instead of the profile's 250-page host cap.
func TestRecrawlSweepBoundsLegacySeedProfileWholeRunBudget(t *testing.T) {
	frontier := openTestFrontier(t)
	profile := seedDueAutomaticDiscoveryURL(t, frontier, "https://seed.example/page", true)

	order := sweepOneDueURL(t, frontier)

	budget := order.EffectiveMaxPagesPerRun(yagocrawlcontract.DefaultMaxPagesPerRun)
	if budget != profile.MaxPagesPerHost {
		t.Fatalf(
			"re-dispatched whole-run budget = %d, want the %d-page host cap",
			budget,
			profile.MaxPagesPerHost,
		)
	}
}

// A manual crawl keeps its ordinary whole-run semantics: an operator profile with
// a host cap and no explicit whole-run cap must not inherit that cap on recrawl.
func TestRecrawlSweepKeepsManualOrderPriority(t *testing.T) {
	frontier := openTestFrontier(t)
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:            "Operator",
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: 250,
		RecrawlIfOlder:  time.Hour,
	})
	ctx := context.Background()
	if err := frontier.RecordProfile(
		ctx,
		profile,
		yagocrawlcontract.CrawlOrderPriorityNormal,
	); err != nil {
		t.Fatalf("record profile: %v", err)
	}
	if err := frontier.RecordFetch(
		ctx,
		"https://op.example/page",
		profile.Handle,
		sweepBase,
	); err != nil {
		t.Fatalf("record fetch: %v", err)
	}

	order := sweepOneDueURL(t, frontier)

	if order.Priority != yagocrawlcontract.CrawlOrderPriorityNormal {
		t.Fatalf("manual re-dispatch priority = %q, want normal", order.Priority)
	}
	if budget := order.EffectiveMaxPagesPerRun(
		yagocrawlcontract.DefaultMaxPagesPerRun,
	); budget != yagocrawlcontract.DefaultMaxPagesPerRun {
		t.Fatalf("manual whole-run budget = %d, want the operator default", budget)
	}
}
