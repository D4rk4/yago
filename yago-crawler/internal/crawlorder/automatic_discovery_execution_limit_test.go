package crawlorder

import (
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func automaticDiscoveryProfile(
	name string,
	depth int,
	maximumPagesPerHost int,
	maximumPagesPerRun *int,
) yagocrawlcontract.CrawlProfile {
	return yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:            name,
		MaxDepth:        depth,
		MaxPagesPerHost: maximumPagesPerHost,
		MaxPagesPerRun:  maximumPagesPerRun,
	})
}

func automaticDiscoveryLimitConsumer(
	limits []yagocrawlcontract.AutomaticDiscoveryExecutionLimit,
) *CrawlOrderConsumer {
	return NewCrawlOrderConsumer(
		boundedqueue.NewBoundedQueue[CrawlOrderDelivery](1),
		frontier.NewFrontier(1, nil),
	).WithAutomaticDiscoveryExecutionLimits(limits)
}

func TestAutomaticDiscoveryExecutionLimitClampsStaleProfileWithoutChangingIdentity(t *testing.T) {
	maximum := 250
	profile := automaticDiscoveryProfile("web-fallback-seed", 5, 250, &maximum)
	limits := []yagocrawlcontract.AutomaticDiscoveryExecutionLimit{
		{ProfileName: "other", MaximumDepth: 1, MaximumPagesPerHost: 1, MaximumPagesPerRun: 1},
		{
			ProfileName:         "web-fallback-seed",
			MaximumDepth:        3,
			MaximumPagesPerHost: 25,
			MaximumPagesPerRun:  25,
		},
	}
	consumer := automaticDiscoveryLimitConsumer(limits)
	limits[1].MaximumPagesPerRun = 200
	compiled, ok := consumer.compileCrawlOrder(t.Context(), yagocrawlcontract.CrawlOrder{
		Priority: yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		Profile:  profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			Mode: yagocrawlcontract.CrawlRequestModeURL,
			URL:  "https://example.com/",
		}},
	}, CrawlOrderDelivery{})
	if !ok {
		t.Fatal("stale automatic discovery order did not compile")
	}
	limited := compiled.Profile
	if limited.MaxDepth != 3 || limited.MaxPagesPerHost != 25 ||
		limited.MaxPagesPerRun == nil || *limited.MaxPagesPerRun != 25 {
		t.Fatalf("limited profile = %+v", limited)
	}
	if limited.Handle != profile.Handle || *profile.MaxPagesPerRun != 250 {
		t.Fatalf("profile identity or source mutated: limited=%+v source=%+v", limited, profile)
	}
}

func TestAutomaticDiscoveryExecutionLimitClampsInheritedAndUnlimitedBudgets(t *testing.T) {
	limit := yagocrawlcontract.AutomaticDiscoveryExecutionLimit{
		ProfileName:         "web-fallback-seed",
		MaximumDepth:        2,
		MaximumPagesPerHost: 25,
		MaximumPagesPerRun:  20,
	}
	consumer := automaticDiscoveryLimitConsumer(
		[]yagocrawlcontract.AutomaticDiscoveryExecutionLimit{limit},
	)
	for name, maximum := range map[string]*int{"inherited": nil, "unlimited": new(int)} {
		t.Run(name, func(t *testing.T) {
			profile := consumer.executionLimitedProfile(yagocrawlcontract.CrawlOrder{
				Priority: yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
				Profile: automaticDiscoveryProfile(
					"web-fallback-seed",
					4,
					yagocrawlcontract.UnlimitedPagesPerHost,
					maximum,
				),
			})
			if profile.MaxDepth != 2 || profile.MaxPagesPerHost != 25 ||
				profile.MaxPagesPerRun == nil || *profile.MaxPagesPerRun != 20 {
				t.Fatalf("limited profile = %+v", profile)
			}
		})
	}
}

func TestAutomaticDiscoveryExecutionLimitDoesNotWidenOrAffectOtherOrders(t *testing.T) {
	maximum := 10
	profile := automaticDiscoveryProfile("web-fallback-seed", 1, 10, &maximum)
	consumer := automaticDiscoveryLimitConsumer(
		[]yagocrawlcontract.AutomaticDiscoveryExecutionLimit{{
			ProfileName:         "web-fallback-seed",
			MaximumDepth:        3,
			MaximumPagesPerHost: 25,
			MaximumPagesPerRun:  20,
		}},
	)
	limited := consumer.executionLimitedProfile(yagocrawlcontract.CrawlOrder{
		Priority: yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		Profile:  profile,
	})
	if limited != profile {
		t.Fatalf("already-correct profile changed: %+v", limited)
	}
	manual := consumer.executionLimitedProfile(
		yagocrawlcontract.CrawlOrder{Profile: automaticDiscoveryProfile(
			"web-fallback-seed",
			5,
			250,
			new(int),
		)},
	)
	if manual.MaxDepth != 5 || manual.MaxPagesPerHost != 250 ||
		manual.MaxPagesPerRun == nil || *manual.MaxPagesPerRun != 0 {
		t.Fatalf("manual profile was limited: %+v", manual)
	}
	unmatched := consumer.executionLimitedProfile(yagocrawlcontract.CrawlOrder{
		Priority: yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		Profile:  automaticDiscoveryProfile("another-profile", 5, 250, new(int)),
	})
	if unmatched.MaxDepth != 5 || unmatched.MaxPagesPerHost != 250 ||
		unmatched.MaxPagesPerRun == nil || *unmatched.MaxPagesPerRun != 0 {
		t.Fatalf("unmatched profile was limited: %+v", unmatched)
	}
}
